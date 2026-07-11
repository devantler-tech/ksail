package workload

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/cli/experimental"
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

// ErrInterceptSteerUnspecified rejects an intercept with neither an explicit
// --steer-command nor a --service-port to derive one from: without one of them
// the steering container has no agent invocation to exec.
var ErrInterceptSteerUnspecified = errors.New(
	"provide --service-port to intercept a workload port (or --steer-command to run a custom agent)",
)

// ErrInvalidInterceptServicePort rejects a --service-port outside the valid TCP
// range or one that collides with the agent's internal listener port (which
// would redirect the port to itself).
var ErrInvalidInterceptServicePort = errors.New(
	"invalid --service-port: must be 1-65535 and differ from the agent's internal port",
)

// defaultSteerWaitTimeout bounds how long intercept waits for the injected
// steering container to reach Running before giving up.
const defaultSteerWaitTimeout = 60 * time.Second

// steerAgentInterceptPort is the pod-internal loopback port the default steering
// agent listens on: its iptables rule redirects the workload's --service-port to
// this port inside the pod's own netns. It is internal (never the developer's
// --local-port), fixed to a high, uncommon value unlikely to clash with the
// workload's own listeners; a --service-port that lands on it is rejected.
const steerAgentInterceptPort = 19000

// loopbackHost is the address the intercepted streams are forwarded to — the
// developer's local process listens on loopback, never a routable interface.
const loopbackHost = "127.0.0.1"

const interceptCmdLong = `Intercept a Deployment's inbound traffic to a local process (reverse dev bridge).

Resolves the Deployment to a running pod, injects a steering container (an
ephemeral container holding only CAP_NET_ADMIN), and runs the steering agent in
it over the Kubernetes exec channel. The agent installs an iptables REDIRECT rule
that captures the workload's inbound TCP and forwards it, over a multiplexed
tunnel, to a process running on your machine — so the copy on your laptop serves
the cluster's live requests.

Unlike ` + "`workload mirror`" + ` (read-only pcap, traffic keeps flowing to the
workload), intercept steers the traffic away from the workload to your local
process; the agent removes its rule again on exit, restoring the pod.

The intercept runs until interrupted. Ctrl-C stops it cleanly with exit status 0,
tearing the tunnel down and letting the in-cluster agent reverse its redirect.

By default intercept runs the KSail-shipped steering image and derives the agent
command from --service-port, so only --service-port and --local-port are needed.
Pass an explicit --steer-image and --steer-command to run a custom agent instead.

Ephemeral containers cannot be removed, so an already-injected steering agent on
the target pod is reused rather than treated as an error.`

const interceptCmdExample = `  # Intercept the traffic my-app serves on :8080 and forward it to localhost:8080
  ksail workload intercept my-app --service-port 8080 --local-port 8080

  # Intercept a specific container of a multi-container Deployment in a namespace
  ksail workload intercept my-app -n prod -c api --service-port 8080 --local-port 8080

  # Run a custom steering agent image and command instead of the KSail default
  ksail workload intercept my-app --local-port 8080 \
    --steer-image ghcr.io/acme/ksail-steer:latest \
    --steer-command ksail-steer --steer-command --port=8080`

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
	keepalive bool,
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

	// Liveness pings let the agent's watchdog distinguish an idle client
	// from a dead one, so an uncleanly killed client cannot orphan the
	// agent and its REDIRECT rule (ksail#6040). Only when the agent
	// provably speaks the keepalive protocol (steerKeepaliveSupported) —
	// an older agent's decoder tears the tunnel down on the unknown frame
	// type. The goroutine ends with ctx / the session; both are torn down
	// before this function returns.
	if keepalive {
		go mirror.SendKeepalives(ctx, session, mirror.SteerKeepaliveInterval)
	}

	err = mirror.ServeIntercepted(ctx, session, localProcessDialer(localPort))
	if err != nil {
		return fmt.Errorf("serve intercepted traffic: %w", err)
	}

	return nil
}

// steerKeepaliveSupported reports whether the steering agent this session
// execs into provably speaks the keepalive protocol (ksail#6040): the steer
// command must be the ksail-derived default (a custom --steer-command may run
// any agent) and the pod's live steering container — which may be a reused
// injection from an older release, since ephemeral containers cannot be
// removed — must run exactly this build's version-pinned
// [mirror.DefaultSteerImage] ([mirror.SteerKeepaliveImageProven]; an
// unstamped dev build's mutable :latest default proves nothing, so it never
// negotiates). Anything else falls back to the pre-keepalive behaviour rather
// than risking the older decoder tearing the tunnel down on an unknown frame
// type; a lookup failure counts as unsupported for the same reason.
func steerKeepaliveSupported(
	ctx context.Context,
	client kubernetes.Interface,
	point *mirror.TapPoint,
	opts interceptOptions,
) bool {
	if !opts.steerCommandDerived {
		return false
	}

	image, err := mirror.SteerContainerImage(ctx, client, point)

	return err == nil && mirror.SteerKeepaliveImageProven(image)
}

// steerSessionCommand returns the agent command to exec for this session:
// the resolved steer command, plus the --expect-keepalives agent flag when
// keepalives are negotiated, so the agent arms its liveness watchdog from
// session start instead of waiting for a first ping that an immediately-dead
// client would never deliver (ksail#6040). The flag is only ever appended to
// the ksail-derived default command against this build's own agent image
// (steerKeepaliveSupported), which is guaranteed to understand it; the slice
// is copied so the resolved options stay untouched.
func steerSessionCommand(steerCommand []string, keepalive bool) []string {
	if !keepalive {
		return steerCommand
	}

	command := make([]string, 0, len(steerCommand)+1)
	command = append(command, steerCommand...)

	return append(command, "--"+mirror.SteerExpectKeepalivesFlag)
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
	servicePort  int
	steerImage   string
	steerCommand []string
	steerTimeout time.Duration
	context      string

	// steerCommandDerived records that resolveSteerCommand derived the
	// default `ksail steer-agent` invocation (no explicit --steer-command).
	// Only a derived command against this build's own steer image provably
	// speaks the keepalive protocol (steerKeepaliveSupported).
	steerCommandDerived bool
}

// NewInterceptCmd creates the workload intercept command (issue #4521,
// increment 3): it wires the pkg/svc/mirror steering primitives — resolve,
// injection-point selection, steering-agent injection, exec-channel transport,
// tunnel session, and intercepted serve — into one dev-loop command.
func NewInterceptCmd() *cobra.Command {
	opts := interceptOptions{}

	cmd := newDeploymentCmd(deploymentCmdMeta{
		use:            "intercept <deployment>",
		short:          "Intercept a Deployment's inbound traffic to a local process (reverse dev bridge)",
		long:           interceptCmdLong,
		example:        interceptCmdExample,
		containerUsage: "Container whose traffic is intercepted (required when the Deployment has several)",
	}, targetFlagRefs{&opts.namespace, &opts.container, &opts.context})

	cmd.Flags().IntVarP(&opts.localPort, "local-port", "l", 0,
		"Local port the intercepted inbound traffic is forwarded to (required)")
	cmd.Flags().IntVar(&opts.servicePort, "service-port", 0,
		"Workload port to intercept; KSail derives the default steering command from it "+
			"(alternative to an explicit --steer-command)")
	cmd.Flags().StringArrayVar(&opts.steerCommand, "steer-command", nil,
		"Command the in-cluster steering agent container runs, exec'd over the tunnel "+
			"(default: derived from --service-port for the KSail steering image)")
	cmd.Flags().StringVar(&opts.steerImage, "steer-image", mirror.DefaultSteerImage,
		"Image the injected steering container runs (must carry the agent binary and iptables)")
	cmd.Flags().DurationVar(&opts.steerTimeout, "wait-timeout", defaultSteerWaitTimeout,
		"How long to wait for the steering container to reach Running")

	_ = cmd.MarkFlagRequired("local-port")

	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if opts.localPort <= 0 || opts.localPort > maxTCPPort {
			return fmt.Errorf("%w: got %d", ErrInvalidInterceptLocalPort, opts.localPort)
		}

		err := resolveSteerCommand(&opts)
		if err != nil {
			return err
		}

		return runInterceptCommand(cmd, args[0], opts)
	}

	// intercept is a reverse dev-bridge (#4521) whose default steering image and
	// derived agent command are now wired (#5945), but the full in-cluster path
	// is only exercisable against a live cluster, so it stays gated experimental
	// until that end-to-end validation graduates it (#5882 AC#3). Graduate by
	// dropping this Guard call.
	return experimental.Guard(cmd)
}

// resolveSteerCommand fills opts.steerCommand when the caller did not pass one
// explicitly: it derives the default `ksail steer-agent` invocation from
// --service-port (honoured by the KSail-shipped steering image). An explicit
// --steer-command is left untouched (the escape hatch for a custom agent); with
// neither flag there is nothing to exec, and a --service-port that is out of
// range or collides with the agent's internal listener port is rejected.
func resolveSteerCommand(opts *interceptOptions) error {
	if len(opts.steerCommand) > 0 {
		return nil
	}

	if opts.servicePort <= 0 {
		return ErrInterceptSteerUnspecified
	}

	if opts.servicePort > maxTCPPort || opts.servicePort == steerAgentInterceptPort {
		return fmt.Errorf("%w: got %d", ErrInvalidInterceptServicePort, opts.servicePort)
	}

	opts.steerCommand = deriveSteerCommand(opts.servicePort)
	opts.steerCommandDerived = true

	return nil
}

// deriveSteerCommand builds the default steering-agent invocation for the
// KSail-shipped steering image (Dockerfile.steer places `ksail` on PATH): run
// `ksail steer-agent` with the workload's service port and the agent's internal
// loopback listener port.
func deriveSteerCommand(servicePort int) []string {
	return []string{
		"ksail", "steer-agent",
		"--service-port=" + strconv.Itoa(servicePort),
		"--intercept-port=" + strconv.Itoa(steerAgentInterceptPort),
	}
}

// runInterceptCommand chains the increment-3 steering primitives end-to-end:
// resolve → select injection point → inject/reuse steering agent → wait →
// tunnel + serve to the local process.
func runInterceptCommand(cmd *cobra.Command, deployment string, opts interceptOptions) error {
	// Install the signal handler before any cluster setup so Ctrl-C also
	// cancels pod resolution, steering-agent injection, and readiness waits.
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	kubeconfigPath := kubeconfig.GetKubeconfigPathSilently(cmd)

	client, restConfig, err := newMirrorClients(kubeconfigPath, opts.context)
	if err != nil {
		return cleanInterceptCancellation(ctx, err)
	}

	point, err := resolveInjectionPoint(
		ctx,
		client,
		opts.namespace,
		opts.container,
		deployment,
	)
	if err != nil {
		return cleanInterceptCancellation(ctx, err)
	}

	err = ensureSteer(ctx, cmd, client, point, opts)
	if err != nil {
		return cleanInterceptCancellation(ctx, err)
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.ActivityType,
		Content: "intercepting inbound traffic to local port %d — press Ctrl-C to stop",
		Args:    []any{opts.localPort},
		Writer:  cmd.ErrOrStderr(),
	})

	keepalive := steerKeepaliveSupported(ctx, client, point, opts)

	err = runInterceptSession(
		ctx,
		client,
		restConfig,
		point,
		steerSessionCommand(opts.steerCommand, keepalive),
		opts.localPort,
		keepalive,
	)

	return cleanInterceptCancellation(ctx, err)
}

// cleanInterceptCancellation maps signal-driven context cancellation to the
// documented successful Ctrl-C exit while preserving unrelated setup errors.
func cleanInterceptCancellation(ctx context.Context, err error) error {
	if errors.Is(ctx.Err(), context.Canceled) && errors.Is(err, context.Canceled) {
		return nil
	}

	return err
}

// ensureSteer injects the steering agent into the injection point's pod —
// reusing an already-injected agent, since ephemeral containers cannot be
// removed — and waits for it to reach Running. The steering container runs its
// inert holder; the operator-supplied agent command is exec'd over the tunnel
// by runInterceptSession, matching the agent's design (its stdin/stdout is the
// exec channel, not the container's entrypoint).
func ensureSteer(
	ctx context.Context,
	cmd *cobra.Command,
	client kubernetes.Interface,
	point *mirror.TapPoint,
	opts interceptOptions,
) error {
	return injectAndWait(ctx, cmd, point, ephemeralInjector{
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
func injectAndWait(
	ctx context.Context,
	cmd *cobra.Command,
	point *mirror.TapPoint,
	inj ephemeralInjector,
) error {
	_, err := inj.inject(ctx)

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

	err = inj.wait(ctx)
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
