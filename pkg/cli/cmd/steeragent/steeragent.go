// Package steeragent provides the hidden `ksail steer-agent` command: the
// in-cluster steering-agent entrypoint that `ksail workload intercept` execs
// inside the ephemeral steering container. It installs the pod's inbound
// REDIRECT rule and forwards the redirected traffic over the tunnel (its
// stdin/stdout, the exec channel) to the developer's local process.
//
// It exists so a KSail-shipped steering image can run the tunnel-speaking agent
// out of the box: the default `--steer-image` (netshoot) carries `iptables` but
// no binary that speaks the tunnel protocol, so `workload intercept` today needs
// an operator-supplied `--steer-command`. This command is that command — the
// concrete os/exec runner, loopback listener, and stdio transport the merged
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

// loopbackHost is the address the steering listener binds to. The agent shares
// the pod's network namespace and the REDIRECT rule retargets traffic here, so
// binding to loopback (not all interfaces) is both sufficient and least-exposure.
const loopbackHost = "127.0.0.1"

// options carries the steering agent's port configuration.
type options struct {
	servicePort   int
	interceptPort int
}

// deps are the injectable seams that keep [run] unit-testable without a live
// network namespace: the byte transport to the ksail side, the listener
// factory, and the iptables command runner. Production wires the concrete
// stdio transport, loopback listener, and os/exec runner via [defaultDeps];
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
service port and forwards the redirected connections over a tunnel — its
stdin/stdout, the exec channel — to a local process on the developer's machine.
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
		"The loopback port the agent listens on and redirects the service port to",
	)

	return cmd
}

// run composes the merged steering seams into a running agent: it builds the
// redirect from opts, opens the loopback listener, and hands both to
// [mirror.RunSteerAgent] with the transport and iptables runner. It blocks
// until ctx is cancelled or the tunnel ends, and returns nil on a graceful stop.
func run(ctx context.Context, opts options, dependencies deps) error {
	redirect := mirror.SteeringRedirect{
		ServicePort:   opts.servicePort,
		InterceptPort: opts.interceptPort,
	}

	err := redirect.Validate()
	if err != nil {
		return fmt.Errorf("validating the steering redirect: %w", err)
	}

	listener, err := dependencies.listen(ctx, opts.interceptPort)
	if err != nil {
		return fmt.Errorf("opening the steering listener on port %d: %w", opts.interceptPort, err)
	}

	defer func() { _ = listener.Close() }()

	err = mirror.RunSteerAgent(ctx, dependencies.transport, listener, redirect, dependencies.runner)
	if err != nil {
		return fmt.Errorf("running the steering agent: %w", err)
	}

	return nil
}

// defaultDeps wires the concrete production seams.
func defaultDeps() deps {
	return deps{
		transport: stdioTransport{},
		listen:    listenLoopback,
		runner:    execRunner,
	}
}

// listenLoopback binds a TCP listener to [loopbackHost] on port.
func listenLoopback(ctx context.Context, port int) (net.Listener, error) {
	var config net.ListenConfig

	listener, err := config.Listen(ctx, "tcp", net.JoinHostPort(loopbackHost, strconv.Itoa(port)))
	if err != nil {
		return nil, fmt.Errorf("binding the loopback listener: %w", err)
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
