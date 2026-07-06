package workload

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v7/pkg/cli/kubeconfig"
	"github.com/devantler-tech/ksail/v7/pkg/notify"
	"github.com/devantler-tech/ksail/v7/pkg/svc/mirror"
	"github.com/spf13/cobra"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// ErrInvalidInterceptLocalPort rejects --local-port values outside the valid
// TCP port range.
var ErrInvalidInterceptLocalPort = errors.New(
	"invalid --local-port: must be between 1 and 65535",
)

// ErrInterceptSteerCommandRequired rejects an empty --steer-command: until
// KSail ships a default steering-agent image the in-cluster agent invocation
// is operator-supplied, so the command has no agent to exec without it.
var ErrInterceptSteerCommandRequired = errors.New(
	"invalid --steer-command: the in-cluster steering agent command is required",
)

// defaultSteerWaitTimeout bounds how long intercept waits for the injected
// steering container to reach Running before giving up.
const defaultSteerWaitTimeout = 60 * time.Second

// loopbackHost is the address the intercepted streams are forwarded to — the
// developer's local process listens on loopback, never a routable interface.
const loopbackHost = "127.0.0.1"

const interceptCmdLong = `Intercept a Deployment's inbound traffic to a local process (reverse dev bridge).

Resolves the Deployment to a running pod, injects a steering container (an
ephemeral container holding only CAP_NET_ADMIN), and runs the operator-supplied
steering agent in it over the Kubernetes exec channel. The agent installs an
iptables REDIRECT rule that captures the workload's inbound TCP and forwards it,
over a multiplexed tunnel, to a process running on your machine — so the copy on
your laptop serves the cluster's live requests.

Unlike ` + "`workload mirror`" + ` (read-only pcap, traffic keeps flowing to the
workload), intercept steers the traffic away from the workload to your local
process; the agent removes its rule again on exit, restoring the pod.

The intercept runs until interrupted (Ctrl-C), which tears the tunnel down and
lets the in-cluster agent reverse its redirect.

--steer-command is required and --steer-image must carry the agent binary plus
iptables: KSail does not yet ship a default steering-agent image (tracked as a
follow-up), so the agent is operator-supplied for now.

Ephemeral containers cannot be removed, so an already-injected steering agent on
the target pod is reused rather than treated as an error.`

const interceptCmdExample = `  # Intercept the traffic my-app receives and forward it to localhost:8080
  ksail workload intercept my-app --local-port 8080 \
    --steer-image ghcr.io/acme/ksail-steer:latest \
    --steer-command ksail-steer --steer-command --port=8080

  # Intercept a specific container of a multi-container Deployment in a namespace
  ksail workload intercept my-app -n prod -c api --local-port 8080 \
    --steer-image ghcr.io/acme/ksail-steer:latest --steer-command ksail-steer`

// runInterceptSession is the blocking client side of intercept the command
// drives: it opens the exec-channel transport to the steering agent, muxes the
// steering tunnel over it, and serves each intercepted stream to the local
// process. It is a package-level seam so tests can substitute the tunnel
// without a live cluster.
//
//nolint:gochecknoglobals // Test seam: lets tests stub the steering tunnel without a live cluster.
var runInterceptSession = defaultRunInterceptSession

// defaultRunInterceptSession is the production runInterceptSession: exec the
// steering agent in the steering container, wrap the exec channel in a
// client-role tunnel session, and serve each stream the agent opens to the
// developer's local process on loopback.
func defaultRunInterceptSession(
	ctx context.Context,
	client kubernetes.Interface,
	restConfig *rest.Config,
	point *mirror.TapPoint,
	steerCommand []string,
	localPort int,
) error {
	transport, err := mirror.OpenExecTransport(
		ctx, client, restConfig, point, mirror.SteerContainerName, steerCommand,
	)
	if err != nil {
		return fmt.Errorf("open steering transport: %w", err)
	}

	defer func() { _ = transport.Close() }()

	session := mirror.NewTunnelSession(transport, transport, mirror.TunnelRoleClient)

	defer func() { _ = session.Close() }()

	err = mirror.ServeIntercepted(ctx, session, localProcessDialer(localPort))
	if err != nil {
		return fmt.Errorf("serve intercepted traffic: %w", err)
	}

	return nil
}

// localProcessDialer builds the LocalDialer intercept forwards each stream to:
// one loopback TCP connection to the developer's process per intercepted flow.
func localProcessDialer(localPort int) mirror.LocalDialer {
	address := net.JoinHostPort(loopbackHost, strconv.Itoa(localPort))

	return func(ctx context.Context) (io.ReadWriteCloser, error) {
		var dialer net.Dialer

		conn, err := dialer.DialContext(ctx, "tcp", address)
		if err != nil {
			return nil, fmt.Errorf("dial local process at %s: %w", address, err)
		}

		return conn, nil
	}
}

// interceptOptions carries the intercept command's flag values into the run
// function.
type interceptOptions struct {
	namespace    string
	container    string
	localPort    int
	steerImage   string
	steerCommand []string
	steerTimeout time.Duration
	context      string
}

// NewInterceptCmd creates the workload intercept command (issue #4521,
// increment 3): it wires the pkg/svc/mirror steering primitives — resolve,
// injection-point selection, steering-agent injection, exec-channel transport,
// tunnel session, and intercepted serve — into one dev-loop command.
func NewInterceptCmd() *cobra.Command {
	opts := interceptOptions{}

	cmd := &cobra.Command{
		Use:          "intercept <deployment>",
		Short:        "Intercept a Deployment's inbound traffic to a local process (reverse dev bridge)",
		Long:         interceptCmdLong,
		Example:      interceptCmdExample,
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		Annotations: map[string]string{
			annotations.AnnotationPermission: permissionWrite,
		},
	}

	cmd.Flags().StringVarP(&opts.namespace, "namespace", "n", "default",
		"Namespace of the target Deployment")
	cmd.Flags().StringVarP(&opts.container, "container", "c", "",
		"Container whose traffic is intercepted (required when the Deployment has several)")
	cmd.Flags().IntVarP(&opts.localPort, "local-port", "l", 0,
		"Local port the intercepted inbound traffic is forwarded to (required)")
	cmd.Flags().StringArrayVar(&opts.steerCommand, "steer-command", nil,
		"Command the in-cluster steering agent container runs, exec'd over the tunnel (required)")
	cmd.Flags().StringVar(&opts.steerImage, "steer-image", mirror.DefaultSteerImage,
		"Image the injected steering container runs (must carry the agent binary and iptables)")
	cmd.Flags().DurationVar(&opts.steerTimeout, "wait-timeout", defaultSteerWaitTimeout,
		"How long to wait for the steering container to reach Running")
	cmd.Flags().StringVar(&opts.context, "context", "",
		"Kubeconfig context of the target cluster")

	_ = cmd.MarkFlagRequired("local-port")
	_ = cmd.MarkFlagRequired("steer-command")

	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if opts.localPort <= 0 || opts.localPort > maxTCPPort {
			return fmt.Errorf("%w: got %d", ErrInvalidInterceptLocalPort, opts.localPort)
		}

		if len(opts.steerCommand) == 0 {
			return ErrInterceptSteerCommandRequired
		}

		return runInterceptCommand(cmd, args[0], opts)
	}

	return cmd
}

// runInterceptCommand chains the increment-3 steering primitives end-to-end:
// resolve → select injection point → inject/reuse steering agent → wait →
// tunnel + serve to the local process.
func runInterceptCommand(cmd *cobra.Command, deployment string, opts interceptOptions) error {
	kubeconfigPath := kubeconfig.GetKubeconfigPathSilently(cmd)

	client, restConfig, err := newMirrorClients(kubeconfigPath, opts.context)
	if err != nil {
		return err
	}

	point, err := resolveInjectionPoint(
		cmd.Context(),
		client,
		opts.namespace,
		opts.container,
		deployment,
	)
	if err != nil {
		return err
	}

	err = ensureSteer(cmd, client, point, opts)
	if err != nil {
		return err
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.ActivityType,
		Content: "intercepting inbound traffic to local port %d — press Ctrl-C to stop",
		Args:    []any{opts.localPort},
		Writer:  cmd.ErrOrStderr(),
	})

	return runInterceptSession(
		cmd.Context(),
		client,
		restConfig,
		point,
		opts.steerCommand,
		opts.localPort,
	)
}

// ensureSteer injects the steering agent into the injection point's pod —
// reusing an already-injected agent, since ephemeral containers cannot be
// removed — and waits for it to reach Running. The steering container runs its
// inert holder; the operator-supplied agent command is exec'd over the tunnel
// by runInterceptSession, matching the agent's design (its stdin/stdout is the
// exec channel, not the container's entrypoint).
func ensureSteer(
	cmd *cobra.Command,
	client kubernetes.Interface,
	point *mirror.TapPoint,
	opts interceptOptions,
) error {
	return injectAndWait(cmd, point, ephemeralInjector{
		inject: func(ctx context.Context) (string, error) {
			return mirror.InjectSteer(ctx, client, point, mirror.WithSteerImage(opts.steerImage))
		},
		wait: func(ctx context.Context) error {
			return mirror.WaitForSteer(ctx, client, point, opts.steerTimeout)
		},
		alreadyErr:  mirror.ErrSteerAlreadyInjected,
		reuseMsg:    "reusing the steering agent already injected on pod %s",
		injectedMsg: "injected steering agent into pod %s (container %s)",
		injectVerb:  "inject steering agent",
		waitVerb:    "wait for steering agent",
	})
}

// ephemeralInjector wires one flavour of ephemeral-container injection (the
// read-only tap or the steering agent) into the shared inject-reuse-wait flow.
// inject and wait bind the flavour's Inject*/WaitFor* calls; the messages and
// error verbs distinguish the two flavours in output.
type ephemeralInjector struct {
	inject      func(context.Context) (string, error)
	wait        func(context.Context) error
	alreadyErr  error
	reuseMsg    string
	injectedMsg string
	injectVerb  string
	waitVerb    string
}

// injectAndWait injects the ephemeral container (reusing an already-injected
// one, since ephemeral containers cannot be removed) and waits for it to reach
// Running. Shared by the mirror tap and the intercept steering agent so both
// take the identical inject → reuse-or-report → wait path.
func injectAndWait(cmd *cobra.Command, point *mirror.TapPoint, inj ephemeralInjector) error {
	_, err := inj.inject(cmd.Context())

	switch {
	case errors.Is(err, inj.alreadyErr):
		notify.WriteMessage(notify.Message{
			Type:    notify.ActivityType,
			Content: inj.reuseMsg,
			Args:    []any{point.Pod},
			Writer:  cmd.OutOrStdout(),
		})
	case err != nil:
		return fmt.Errorf("%s: %w", inj.injectVerb, err)
	default:
		notify.WriteMessage(notify.Message{
			Type:    notify.ActivityType,
			Content: inj.injectedMsg,
			Args:    []any{point.Pod, point.Container},
			Writer:  cmd.OutOrStdout(),
		})
	}

	err = inj.wait(cmd.Context())
	if err != nil {
		return fmt.Errorf("%s: %w", inj.waitVerb, err)
	}

	return nil
}

// resolveInjectionPoint resolves the Deployment to the concrete (pod,
// container) an ephemeral tap or steering container attaches to. Shared by the
// mirror and intercept commands so both select the injection point identically.
func resolveInjectionPoint(
	ctx context.Context,
	client kubernetes.Interface,
	namespace, container, deployment string,
) (*mirror.TapPoint, error) {
	target, err := mirror.ResolveTarget(ctx, client, namespace, deployment)
	if err != nil {
		return nil, fmt.Errorf("resolve target: %w", err)
	}

	point, err := mirror.SelectTapPoint(target, container)
	if err != nil {
		return nil, fmt.Errorf("select injection point: %w", err)
	}

	return point, nil
}
