// Package steeragent provides the hidden `ksail steer-agent` command: the
// in-cluster steering-agent entrypoint that `ksail workload intercept` execs
// inside the ephemeral steering container. It installs the pod's inbound
// steering rules (the NAT REDIRECT plus its intercept-port guard) and forwards
// the redirected traffic over the tunnel (its stdin/stdout, the exec channel)
// to the developer's local process.
//
// It exists so a KSail-shipped steering image can run the tunnel-speaking agent
// out of the box: the default `--steer-image` (netshoot) carries `iptables` but
// no binary that speaks the tunnel protocol, so `workload intercept` today needs
// an operator-supplied `--steer-command`. This command is that command — the
// concrete os/exec runner, intercept listener, and stdio transport the merged
// [mirror.RunSteerAgent] composition (ksail#5871) documented as "supplied by the
// CLI wiring that launches the agent" (ksail#5851 / #5882). It is intended to be
// launched by the steering image, not run directly, so it is hidden.
package steeragent

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/devantler-tech/ksail/v7/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v7/pkg/svc/mirror"
	"github.com/spf13/cobra"
)

// listenHost is the address the steering listener binds to: all IPv4
// interfaces, NOT loopback. For traffic arriving from outside the pod,
// iptables REDIRECT rewrites the destination to the address of the interface
// the packet came in on (the pod IP) — never to 127.0.0.1 — so a loopback
// listener would refuse every intercepted connection (#6039). Least exposure
// is re-established by the agent's INPUT guard rule, which drops direct
// (non-REDIRECTed) hits on the intercept port.
const listenHost = "0.0.0.0"

// options carries the steering agent's port and liveness configuration.
type options struct {
	servicePort      int
	interceptPort    int
	expectKeepalives bool
}

// deps are the injectable seams that keep [run] unit-testable without a live
// network namespace: the byte transport to the ksail side, the listener
// factory, and the iptables command runner. Production wires the concrete
// stdio transport, intercept listener, and os/exec runner via [defaultDeps];
// tests inject fakes.
type deps struct {
	transport io.ReadWriteCloser
	listen    func(ctx context.Context, port int) (net.Listener, error)
	runner    mirror.SteerCommandRunner
}

// NewSteerAgentCmd creates the hidden `ksail steer-agent` command.
func NewSteerAgentCmd() *cobra.Command {
	var opts options

	cmd := &cobra.Command{
		Use:   "steer-agent",
		Short: "Run the KSail traffic-steering agent inside a pod",
		Long: `Run the KSail traffic-steering agent.

The steering agent runs inside an ephemeral container that shares a target pod's
network namespace. It installs an iptables NAT REDIRECT rule for the workload's
service port (plus a guard that drops direct hits on the intercept port) and
forwards the redirected connections over a tunnel — its stdin/stdout, the exec
channel — to a local process on the developer's machine.
It is execed by ` + "`ksail workload intercept`" + ` via the KSail steering image
rather than run directly.`,
		Hidden: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// A signal-cancelled context is required, not optional: the agent
			// runs in an ephemeral container that cannot be removed, so on pod
			// deletion (SIGTERM) it must reach RunSteerAgent's teardown and
			// delete the REDIRECT rule — otherwise the pod's traffic stays
			// redirected to a dead agent.
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			return run(ctx, opts, defaultDeps())
		},
		Annotations: map[string]string{
			annotations.AnnotationExclude: "true",
		},
	}

	cmd.Flags().IntVar(
		&opts.servicePort,
		"service-port",
		0,
		"The workload port whose inbound TCP is steered",
	)
	cmd.Flags().IntVar(
		&opts.interceptPort,
		"intercept-port",
		0,
		"The port the agent listens on and redirects the service port to",
	)
	cmd.Flags().BoolVar(
		&opts.expectKeepalives,
		mirror.SteerExpectKeepalivesFlag,
		false,
		"Arm the client-liveness watchdog from session start"+
			" (set by intercept clients that will send keepalive pings)",
	)

	return cmd
}

// run composes the merged steering seams into a running agent: it builds the
// redirect and hands the listener factory, transport, and iptables runner to
// [mirror.RunSteerAgent]. That composition installs the guard before opening
// the all-interfaces listener. It blocks until ctx is cancelled or the tunnel
// ends, and returns nil on a graceful stop.
func run(ctx context.Context, opts options, dependencies deps) error {
	redirect := mirror.SteeringRedirect{
		ServicePort:   opts.servicePort,
		InterceptPort: opts.interceptPort,
	}

	err := redirect.Validate()
	if err != nil {
		return fmt.Errorf("validating the steering redirect: %w", err)
	}

	err = mirror.RunSteerAgent(
		ctx,
		dependencies.transport,
		dependencies.listen,
		redirect,
		dependencies.runner,
		opts.expectKeepalives,
	)
	if err != nil {
		return fmt.Errorf("running the steering agent: %w", err)
	}

	return nil
}

// defaultDeps wires the concrete production seams.
func defaultDeps() deps {
	return deps{
		transport: stdioTransport{},
		listen:    listenIntercept,
		runner:    execRunner,
	}
}

// listenIntercept binds a TCP listener to [listenHost] on port. The network is
// pinned to tcp4 — a plain "tcp" wildcard bind can yield a dual-stack socket
// that also accepts direct IPv6 connections, which the IPv4 iptables guard
// rule cannot drop; the REDIRECT rule only ever delivers IPv4.
func listenIntercept(ctx context.Context, port int) (net.Listener, error) {
	var config net.ListenConfig

	listener, err := config.Listen(ctx, "tcp4", net.JoinHostPort(listenHost, strconv.Itoa(port)))
	if err != nil {
		return nil, fmt.Errorf("binding the intercept listener: %w", err)
	}

	return listener, nil
}

// execRunner runs iptables in the agent's own (the pod's) network namespace.
func execRunner(ctx context.Context, name string, args ...string) error {
	// #nosec G204 -- the program name is the constant "iptables" the mirror
	// package hands the runner and args come from a validated SteeringRedirect;
	// os/exec is used directly (no shell), so the argv is passed literally
	// rather than shell-interpreted.
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr

	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("running %s: %w", name, err)
	}

	return nil
}

// stdioTransport bridges the process's stdin and stdout into the
// io.ReadWriteCloser the tunnel expects: when the agent is the steering
// container's process, its stdin/stdout ARE the exec channel to the ksail side.
// Close is a no-op — process exit closes the streams.
type stdioTransport struct{}

func (stdioTransport) Read(p []byte) (int, error) {
	count, err := os.Stdin.Read(p)
	if err != nil {
		return count, fmt.Errorf("reading the exec channel: %w", err)
	}

	return count, nil
}

func (stdioTransport) Write(p []byte) (int, error) {
	count, err := os.Stdout.Write(p)
	if err != nil {
		return count, fmt.Errorf("writing the exec channel: %w", err)
	}

	return count, nil
}

func (stdioTransport) Close() error { return nil }
