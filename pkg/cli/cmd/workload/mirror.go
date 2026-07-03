package workload

import (
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v7/pkg/cli/kubeconfig"
	k8sutil "github.com/devantler-tech/ksail/v7/pkg/k8s"
	"github.com/devantler-tech/ksail/v7/pkg/notify"
	"github.com/devantler-tech/ksail/v7/pkg/svc/mirror"
	"github.com/spf13/cobra"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// defaultTapWaitTimeout bounds how long the mirror waits for the injected tap
// container to reach Running before giving up.
const defaultTapWaitTimeout = 60 * time.Second

// stdoutOutput is the --output value that streams the raw pcap to stdout.
const stdoutOutput = "-"

// defaultMirrorOutput is the pcap file the capture is written to when
// --output is not given.
const defaultMirrorOutput = "mirror.pcap"

const mirrorCmdLong = `Mirror a Deployment's inbound traffic to the local machine (read-only).

Resolves the Deployment to a running pod, injects a read-only tap (an ephemeral
container holding only CAP_NET_RAW), and streams a tcpdump pcap capture of the
service port over the Kubernetes exec channel — no reverse tunnel, no traffic
interception, the workload keeps serving untouched.

The capture runs until interrupted (Ctrl-C). By default it is written to
mirror.pcap and summarized on stop; --output - streams the raw pcap to stdout
for piping into tshark/wireshark.

Ephemeral containers cannot be removed, so an already-injected tap on the
target pod is reused rather than treated as an error.`

const mirrorCmdExample = `  # Mirror the traffic your app receives on port 8080 into mirror.pcap
  ksail workload mirror my-app --port 8080

  # Mirror a specific container of a multi-container Deployment in a namespace
  ksail workload mirror my-app -n prod -c api --port 8080

  # Pipe the live capture straight into tshark
  ksail workload mirror my-app --port 8080 --output - | tshark -r -`

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

// mirrorOptions carries the mirror command's flag values into the run function.
type mirrorOptions struct {
	namespace  string
	container  string
	port       int
	output     string
	tapImage   string
	tapTimeout time.Duration
	context    string
}

// NewMirrorCmd creates the workload mirror command (issue #4521, Phase 1
// mirror-only mode): it wires the pkg/svc/mirror primitives — resolve, tap
// selection, tap injection, capture session, summary — into one dev-loop
// command.
func NewMirrorCmd() *cobra.Command {
	opts := mirrorOptions{}

	cmd := &cobra.Command{
		Use:          "mirror <deployment>",
		Short:        "Mirror a Deployment's inbound traffic locally (read-only pcap capture)",
		Long:         mirrorCmdLong,
		Example:      mirrorCmdExample,
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		Annotations: map[string]string{
			annotations.AnnotationPermission: permissionWrite,
		},
	}

	cmd.Flags().StringVarP(&opts.namespace, "namespace", "n", "default",
		"Namespace of the target Deployment")
	cmd.Flags().StringVarP(&opts.container, "container", "c", "",
		"Container whose traffic is mirrored (required when the Deployment has several)")
	cmd.Flags().IntVarP(&opts.port, "port", "p", 0,
		"Service port to capture TCP traffic on (required)")
	cmd.Flags().StringVarP(&opts.output, "output", "o", defaultMirrorOutput,
		"Destination pcap file, or '-' to stream the raw pcap to stdout")
	cmd.Flags().StringVar(&opts.tapImage, "tap-image", mirror.DefaultTapImage,
		"Image the injected tap container runs")
	cmd.Flags().DurationVar(&opts.tapTimeout, "tap-timeout", defaultTapWaitTimeout,
		"How long to wait for the tap container to reach Running")
	cmd.Flags().StringVar(&opts.context, "context", "",
		"Kubeconfig context of the target cluster")

	_ = cmd.MarkFlagRequired("port")

	cmd.RunE = func(cmd *cobra.Command, args []string) error {
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
// tap attaches to.
func resolveTapPoint(
	cmd *cobra.Command,
	client kubernetes.Interface,
	deployment string,
	opts mirrorOptions,
) (*mirror.TapPoint, error) {
	target, err := mirror.ResolveTarget(cmd.Context(), client, opts.namespace, deployment)
	if err != nil {
		return nil, fmt.Errorf("resolve mirror target: %w", err)
	}

	point, err := mirror.SelectTapPoint(target, opts.container)
	if err != nil {
		return nil, fmt.Errorf("select tap point: %w", err)
	}

	return point, nil
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
	_, err := mirror.InjectTap(
		cmd.Context(), client, point, mirror.WithTapImage(opts.tapImage),
	)

	switch {
	case errors.Is(err, mirror.ErrTapAlreadyInjected):
		notify.WriteMessage(notify.Message{
			Type:    notify.ActivityType,
			Content: "reusing the tap already injected on pod %s",
			Args:    []any{point.Pod},
			Writer:  cmd.OutOrStdout(),
		})
	case err != nil:
		return fmt.Errorf("inject tap: %w", err)
	default:
		notify.WriteMessage(notify.Message{
			Type:    notify.ActivityType,
			Content: "injected read-only tap into pod %s (container %s)",
			Args:    []any{point.Pod, point.Container},
			Writer:  cmd.OutOrStdout(),
		})
	}

	err = mirror.WaitForTap(cmd.Context(), client, point, opts.tapTimeout)
	if err != nil {
		return fmt.Errorf("wait for tap: %w", err)
	}

	return nil
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

	notify.WriteMessage(notify.Message{
		Type:    notify.ActivityType,
		Content: "capturing TCP traffic on port %d — press Ctrl-C to stop",
		Args:    []any{opts.port},
		Writer:  cmd.ErrOrStderr(),
	})

	captureErr := runCaptureSession(
		cmd.Context(), client, restConfig, point, opts.port, out,
	)

	if file != nil {
		closeErr := file.Close()
		if captureErr == nil && closeErr != nil {
			captureErr = fmt.Errorf("close capture file: %w", closeErr)
		}
	}

	if captureErr != nil {
		return fmt.Errorf("capture session: %w", captureErr)
	}

	if file == nil {
		return nil
	}

	return summarizeMirrorFile(cmd, file.Name())
}

// openMirrorOutput opens the capture destination: the command's stdout for
// "-", else the named file (also returned so the caller can close and
// summarize it).
func openMirrorOutput(cmd *cobra.Command, output string) (io.Writer, *os.File, error) {
	if output == stdoutOutput {
		return cmd.OutOrStdout(), nil, nil
	}

	file, err := os.Create(output) //nolint:gosec // G304: the path IS the user's --output flag.
	if err != nil {
		return nil, nil, fmt.Errorf("create capture file: %w", err)
	}

	return file, file, nil
}

// summarizeMirrorFile re-reads the finished capture file and reports how much
// was captured.
func summarizeMirrorFile(cmd *cobra.Command, path string) error {
	file, err := os.Open(path) //nolint:gosec // G304: re-reads the capture file this command just wrote.
	if err != nil {
		return fmt.Errorf("open capture file for summary: %w", err)
	}

	defer func() { _ = file.Close() }()

	summary, err := mirror.SummarizeCapture(file)
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
