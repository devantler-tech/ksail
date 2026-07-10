package workload

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v7/pkg/cli/kubeconfig"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	k8sutil "github.com/devantler-tech/ksail/v7/pkg/k8s"
	"github.com/devantler-tech/ksail/v7/pkg/notify"
	"github.com/devantler-tech/ksail/v7/pkg/svc/mirror"
	"github.com/spf13/cobra"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// ErrInvalidMirrorPort rejects --port values outside the valid TCP port range.
var ErrInvalidMirrorPort = errors.New("invalid --port: must be between 1 and 65535")

// ErrInvalidMirrorReplayTarget rejects --to values that are not host:port.
var ErrInvalidMirrorReplayTarget = errors.New("invalid --to: must be host:port")

// defaultTapWaitTimeout bounds how long the mirror waits for the injected tap
// container to reach Running before giving up.
const defaultTapWaitTimeout = 60 * time.Second

// stdoutOutput is the --output value that streams the raw pcap to stdout.
const stdoutOutput = "-"

// defaultMirrorOutput is the pcap file the capture is written to when
// --output is not given.
const defaultMirrorOutput = "mirror.pcap"

// maxTCPPort is the highest valid TCP port number.
const maxTCPPort = 65535

// mirrorOutputDirPerm is the mode nested --output directories are created with.
const mirrorOutputDirPerm = 0o750

const mirrorCmdLong = `Mirror a Deployment's inbound traffic to the local machine (read-only).

Resolves the Deployment to a running pod, injects a read-only tap (an ephemeral
container holding only CAP_NET_RAW), and streams a tcpdump pcap capture of the
service port over the Kubernetes exec channel — no reverse tunnel, no traffic
interception, the workload keeps serving untouched.

The capture runs until interrupted. Ctrl-C stops it cleanly with exit status 0.
By default it is written to mirror.pcap and summarized on stop; --output -
streams the raw pcap to stdout for piping into tshark/wireshark.

With --to, the mirrored inbound TCP payloads are additionally replayed LIVE to
a locally-running process (one local connection per mirrored flow) while the
capture runs — the dev-loop bridge: your local service receives the same
requests the cluster workload is serving. Replay is one-way by design: the
local process's responses are read and discarded, nothing flows back into the
cluster.

Ephemeral containers cannot be removed, so an already-injected tap on the
target pod is reused rather than treated as an error.`

const mirrorCmdExample = `  # Mirror the traffic your app receives on port 8080 into mirror.pcap
  ksail workload mirror my-app --port 8080

  # Mirror a specific container of a multi-container Deployment in a namespace
  ksail workload mirror my-app -n prod -c api --port 8080

  # Pipe the live capture straight into tshark
  ksail workload mirror my-app --port 8080 --output - | tshark -r -

  # Replay the mirrored requests live to the copy running on your machine
  ksail workload mirror my-app --port 8080 --to localhost:8080`

// newMirrorClients builds the Kubernetes clientset and REST config the mirror
// command talks to the cluster with. It is a package-level seam so tests can
// substitute a fake clientset and stub exec transport.
//
//nolint:gochecknoglobals // Test seam: lets tests inject a fake clientset without a live cluster.
var newMirrorClients = func(
	kubeconfigPath string,
	contextName string,
) (kubernetes.Interface, *rest.Config, error) {
	client, err := k8sutil.NewClientset(kubeconfigPath, contextName)
	if err != nil {
		return nil, nil, fmt.Errorf("create Kubernetes client: %w", err)
	}

	restConfig, err := k8sutil.BuildRESTConfig(kubeconfigPath, contextName)
	if err != nil {
		return nil, nil, fmt.Errorf("build REST config: %w", err)
	}

	return client, restConfig, nil
}

// runCaptureSession is the blocking capture call the mirror command drives; a
// package-level seam so tests can substitute the exec-channel stream.
//
//nolint:gochecknoglobals // Test seam: lets tests stub the exec-channel capture stream.
var runCaptureSession = mirror.RunCaptureSession

// newLiveReplay builds the live replay sink for --to; a package-level seam so
// tests can inject a capturing dialer.
//
//nolint:gochecknoglobals // Test seam: lets tests observe replayed connections.
var newLiveReplay = func(address string, port int) (*mirror.LiveReplay, error) {
	return mirror.NewLiveReplay(address, port)
}

// mirrorOptions carries the mirror command's flag values into the run function.
type mirrorOptions struct {
	namespace  string
	container  string
	port       int
	output     string
	replayTo   string
	tapImage   string
	tapTimeout time.Duration
	context    string
}

// deploymentCmdMeta carries the per-command metadata newDeploymentCmd needs to
// build a Deployment-targeting dev-loop command.
type deploymentCmdMeta struct {
	use            string
	short          string
	long           string
	example        string
	containerUsage string
}

// targetFlagRefs points the shared namespace/container/context flags at a
// command's own option fields.
type targetFlagRefs struct {
	namespace *string
	container *string
	context   *string
}

// newDeploymentCmd builds a dev-loop command that targets a single Deployment,
// wiring the namespace/container/context flags every such command shares (mirror
// and intercept). The caller adds its own command-specific flags and RunE.
func newDeploymentCmd(meta deploymentCmdMeta, refs targetFlagRefs) *cobra.Command {
	cmd := &cobra.Command{
		Use:          meta.use,
		Short:        meta.short,
		Long:         meta.long,
		Example:      meta.example,
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		Annotations: map[string]string{
			annotations.AnnotationPermission: permissionWrite,
		},
	}

	cmd.Flags().StringVarP(refs.namespace, "namespace", "n", "default",
		"Namespace of the target Deployment")
	cmd.Flags().StringVarP(refs.container, "container", "c", "", meta.containerUsage)
	cmd.Flags().StringVar(refs.context, "context", "",
		"Kubeconfig context of the target cluster")

	return cmd
}

// NewMirrorCmd creates the workload mirror command (issue #4521, Phase 1
// mirror-only mode): it wires the pkg/svc/mirror primitives — resolve, tap
// selection, tap injection, capture session, summary — into one dev-loop
// command.
func NewMirrorCmd() *cobra.Command {
	opts := mirrorOptions{}

	cmd := newDeploymentCmd(deploymentCmdMeta{
		use:            "mirror <deployment>",
		short:          "Mirror a Deployment's inbound traffic locally (read-only pcap capture)",
		long:           mirrorCmdLong,
		example:        mirrorCmdExample,
		containerUsage: "Container whose traffic is mirrored (required when the Deployment has several)",
	}, targetFlagRefs{&opts.namespace, &opts.container, &opts.context})

	cmd.Flags().IntVarP(&opts.port, "port", "p", 0,
		"Service port to capture TCP traffic on (required)")
	cmd.Flags().StringVarP(&opts.output, "output", "o", defaultMirrorOutput,
		"Destination pcap file, or '-' to stream the raw pcap to stdout")
	cmd.Flags().StringVar(&opts.replayTo, "to", "",
		"Local address (host:port) the mirrored inbound TCP payloads are replayed to, live")
	cmd.Flags().StringVar(&opts.tapImage, "tap-image", mirror.DefaultTapImage,
		"Image the injected tap container runs")
	cmd.Flags().DurationVar(&opts.tapTimeout, "tap-timeout", defaultTapWaitTimeout,
		"How long to wait for the tap container to reach Running")

	_ = cmd.MarkFlagRequired("port")

	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if opts.port <= 0 || opts.port > maxTCPPort {
			return fmt.Errorf("%w: got %d", ErrInvalidMirrorPort, opts.port)
		}

		if opts.replayTo != "" {
			_, _, err := net.SplitHostPort(opts.replayTo)
			if err != nil {
				return fmt.Errorf("%w: got %q", ErrInvalidMirrorReplayTarget, opts.replayTo)
			}
		}

		return runMirrorCommand(cmd, args[0], opts)
	}

	return cmd
}

// runMirrorCommand chains the Phase-1 mirror primitives end-to-end: resolve →
// select tap point → inject/reuse tap → wait → capture → summarize.
func runMirrorCommand(cmd *cobra.Command, deployment string, opts mirrorOptions) error {
	kubeconfigPath := kubeconfig.GetKubeconfigPathSilently(cmd)

	client, restConfig, err := newMirrorClients(kubeconfigPath, opts.context)
	if err != nil {
		return err
	}

	point, err := resolveTapPoint(cmd, client, deployment, opts)
	if err != nil {
		return err
	}

	err = ensureTap(cmd, client, point, opts)
	if err != nil {
		return err
	}

	return captureToOutput(cmd, client, restConfig, point, opts)
}

// resolveTapPoint resolves the Deployment to the concrete (pod, container) the
// tap attaches to, via the shared injection-point resolver.
func resolveTapPoint(
	cmd *cobra.Command,
	client kubernetes.Interface,
	deployment string,
	opts mirrorOptions,
) (*mirror.TapPoint, error) {
	return resolveInjectionPoint(cmd.Context(), client, opts.namespace, opts.container, deployment)
}

// ensureTap injects the read-only tap into the tap point's pod — reusing an
// already-injected tap, since ephemeral containers cannot be removed — and
// waits for it to reach Running.
func ensureTap(
	cmd *cobra.Command,
	client kubernetes.Interface,
	point *mirror.TapPoint,
	opts mirrorOptions,
) error {
	return injectAndWait(cmd, point, ephemeralInjector{
		inject: func(ctx context.Context) (string, error) {
			return mirror.InjectTap(ctx, client, point, mirror.WithTapImage(opts.tapImage))
		},
		wait: func(ctx context.Context) error {
			return mirror.WaitForTap(ctx, client, point, opts.tapTimeout)
		},
		alreadyErr:  mirror.ErrTapAlreadyInjected,
		reuseMsg:    "reusing the tap already injected on pod %s",
		injectedMsg: "injected read-only tap into pod %s (container %s)",
		injectVerb:  "inject tap",
		waitVerb:    "wait for tap",
	})
}

// captureToOutput runs the blocking capture session into the configured
// destination and, for file destinations, prints a summary of what was
// captured once the session ends.
func captureToOutput(
	cmd *cobra.Command,
	client kubernetes.Interface,
	restConfig *rest.Config,
	point *mirror.TapPoint,
	opts mirrorOptions,
) error {
	out, file, err := openMirrorOutput(cmd, opts.output)
	if err != nil {
		return err
	}

	out, replay, err := setupLiveReplay(cmd, opts, out)
	if err != nil {
		if file != nil {
			_ = file.Close()
		}

		return err
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.ActivityType,
		Content: "capturing TCP traffic on port %d — press Ctrl-C to stop",
		Args:    []any{opts.port},
		Writer:  cmd.ErrOrStderr(),
	})

	// Ctrl-C is the documented way to end the capture: cancel the session's
	// context on SIGINT/SIGTERM instead of letting the default disposition
	// kill the process, so the session unwinds, the replay drains, the pcap
	// closes, and the summary still prints (the service layer maps a
	// cancelled context to a clean stop).
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	captureErr := runCaptureSession(
		ctx, client, restConfig, point, opts.port, out,
	)

	captureErr = finishCapture(captureErr, replay, file)
	if captureErr != nil {
		return fmt.Errorf("capture session: %w", captureErr)
	}

	if file == nil {
		return nil
	}

	return summarizeMirrorFile(cmd, file.Name())
}

// setupLiveReplay tees the capture stream into a live replay sink when --to
// is set; without it the capture writer passes through untouched.
func setupLiveReplay(
	cmd *cobra.Command,
	opts mirrorOptions,
	out io.Writer,
) (io.Writer, *mirror.LiveReplay, error) {
	if opts.replayTo == "" {
		return out, nil, nil
	}

	replay, err := newLiveReplay(opts.replayTo, opts.port)
	if err != nil {
		return nil, nil, fmt.Errorf("start live replay: %w", err)
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.ActivityType,
		Content: "replaying mirrored inbound traffic to %s",
		Args:    []any{opts.replayTo},
		Writer:  cmd.ErrOrStderr(),
	})

	return io.MultiWriter(out, replay), replay, nil
}

// finishCapture drains the replay sink and closes the capture file, keeping
// the first error in capture → replay → file-close precedence.
func finishCapture(captureErr error, replay *mirror.LiveReplay, file *os.File) error {
	if replay != nil {
		replayErr := replay.Close()
		if captureErr == nil && replayErr != nil {
			captureErr = fmt.Errorf("live replay: %w", replayErr)
		}
	}

	if file != nil {
		closeErr := file.Close()
		if captureErr == nil && closeErr != nil {
			captureErr = fmt.Errorf("close capture file: %w", closeErr)
		}
	}

	return captureErr
}

// openMirrorOutput opens the capture destination: the command's stdout for
// "-", else the named file (also returned so the caller can close and
// summarize it).
func openMirrorOutput(cmd *cobra.Command, output string) (io.Writer, *os.File, error) {
	if output == stdoutOutput {
		return cmd.OutOrStdout(), nil, nil
	}

	canonical, err := prepareMirrorOutputPath(output)
	if err != nil {
		return nil, nil, err
	}

	//nolint:gosec // G304: canonicalized via EvalCanonicalPath above.
	file, err := os.Create(canonical)
	if err != nil {
		return nil, nil, fmt.Errorf("create capture file: %w", err)
	}

	return file, file, nil
}

// prepareMirrorOutputPath creates the output's parent directory if needed and
// canonicalizes the path via EvalCanonicalPath to prevent symlink-escape
// attacks (canonicalize after MkdirAll so the parent exists for resolution).
func prepareMirrorOutputPath(outputPath string) (string, error) {
	outputDir := filepath.Dir(outputPath)
	if outputDir != "." && outputDir != "" {
		err := os.MkdirAll(outputDir, mirrorOutputDirPerm)
		if err != nil {
			return "", fmt.Errorf("create capture output directory: %w", err)
		}
	}

	canonical, err := fsutil.EvalCanonicalPath(outputPath)
	if err != nil {
		return "", fmt.Errorf("resolve capture output path %q: %w", outputPath, err)
	}

	return canonical, nil
}

// summarizeMirrorFile re-reads the finished capture file and reports how much
// was captured.
func summarizeMirrorFile(cmd *cobra.Command, path string) error {
	data, err := fsutil.ReadFileSafe(filepath.Dir(path), path)
	if err != nil {
		return fmt.Errorf("read capture file for summary: %w", err)
	}

	summary, err := mirror.SummarizeCapture(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("summarize capture: %w", err)
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.SuccessType,
		Content: "captured %d packets (%d bytes) to %s",
		Args:    []any{summary.Packets, summary.Bytes, path},
		Writer:  cmd.OutOrStdout(),
	})

	return nil
}
