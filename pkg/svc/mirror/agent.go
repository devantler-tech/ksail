package mirror

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"time"
)

// steerIptablesBinary is the command the steering agent drives to install and
// remove its NAT REDIRECT rule; the steering image ([DefaultSteerImage]) ships
// it. It is the name RunSteerAgent hands the [SteerCommandRunner]; the runner
// owns how it is actually launched.
const steerIptablesBinary = "iptables"

// steerTeardownTimeout bounds the rule-removal command so a wedged iptables at
// shutdown cannot hang the agent's teardown forever.
const steerTeardownTimeout = 10 * time.Second

// ErrSteerTransportNil is returned when RunSteerAgent is called without a
// tunnel transport — the agent has no byte pipe to the ksail side.
var ErrSteerTransportNil = errors.New("steering tunnel transport must not be nil")

// ErrSteerListenerNil is returned when RunSteerAgent is called without a
// listener for the redirected connections.
var ErrSteerListenerNil = errors.New("steering listener must not be nil")

// ErrSteerRunnerNil is returned when RunSteerAgent is called without a command
// runner — the agent cannot install or remove its redirect rule without one.
var ErrSteerRunnerNil = errors.New("steering command runner must not be nil")

// SteerCommandRunner runs a command in the steering agent's own network
// namespace — in production the container's `iptables`, in tests a fake. It is
// the seam that keeps [RunSteerAgent] unit-testable without a live netns and
// keeps this increment free of a subprocess dependency: the concrete os/exec
// runner, the listener bind, and the command the ksail-steer container execs
// are supplied by the CLI wiring that launches the agent (ksail#5851). A
// non-nil error from the install call aborts the agent before it forwards.
type SteerCommandRunner func(ctx context.Context, name string, args ...string) error

// RunSteerAgent is the steering agent's in-container entrypoint composition: it
// installs the iptables NAT REDIRECT rule for redirect, accepts the redirected
// connections on listener and forwards each over the tunnel
// ([ForwardRedirected]), and — win or lose — removes the rule again before
// returning. Reversible teardown is a hard requirement of the #5839 design:
// an ephemeral container cannot be removed, so its rules must be, or the pod's
// traffic stays redirected to a dead agent.
//
// transport is the agent's byte pipe to the ksail side; when the agent runs as
// the ksail-steer container's process it is the exec channel
// [OpenExecTransport] opens, and tests pair it with the ksail side over an
// in-memory pipe. listener receives the connections the in-namespace REDIRECT
// delivers. runner installs and removes the rule (see [SteerCommandRunner]).
//
// It blocks until ctx is cancelled or the tunnel session ends (both return
// nil, matching [ForwardRedirected]) or the listener fails (returns the error).
// The rule teardown runs on a context detached from ctx and time-bounded, so a
// cancelled agent still cleans up; a teardown failure is joined onto any
// forwarding error (via [errors.Join]) and surfaced as the returned error, so a
// dangling REDIRECT rule on an ephemeral pod is observable rather than silent —
// even when forwarding failed too.
func RunSteerAgent(
	ctx context.Context,
	transport io.ReadWriteCloser,
	listener net.Listener,
	redirect SteeringRedirect,
	runner SteerCommandRunner,
) (err error) {
	err = checkSteerAgentInputs(transport, listener, runner)
	if err != nil {
		return err
	}

	insertArgs, err := redirect.InsertArgs()
	if err != nil {
		return fmt.Errorf("building the steering install rule: %w", err)
	}

	err = runner(ctx, steerIptablesBinary, insertArgs...)
	if err != nil {
		return fmt.Errorf("installing the steering redirect rule: %w", err)
	}

	defer func() {
		teardownErr := removeSteeringRule(ctx, redirect, runner)
		if teardownErr != nil {
			// Join rather than only surfacing teardown when forwarding
			// succeeded: a teardown failure that coincides with a forwarding
			// failure still leaves a dangling REDIRECT rule on an ephemeral
			// pod, so both must be observable, not just the first.
			err = errors.Join(err, teardownErr)
		}
	}()

	session := NewTunnelSession(transport, transport, TunnelRoleServer)

	defer func() { _ = session.Close() }()

	return ForwardRedirected(ctx, listener, session)
}

// checkSteerAgentInputs rejects the nil dependencies that would otherwise
// panic once the agent starts forwarding, so a misuse fails with a clear error
// instead of a crash mid-stream.
func checkSteerAgentInputs(
	transport io.ReadWriteCloser,
	listener net.Listener,
	runner SteerCommandRunner,
) error {
	if transport == nil {
		return ErrSteerTransportNil
	}

	if listener == nil {
		return ErrSteerListenerNil
	}

	if runner == nil {
		return ErrSteerRunnerNil
	}

	return nil
}

// removeSteeringRule runs the redirect's delete rule on a context detached from
// the agent's own — the agent stops precisely because ctx was cancelled, so
// reusing it would kill the teardown command before it could undo the rule. It
// returns the build-or-run failure rather than swallowing it: an ephemeral
// container cannot be removed, so a rule the container failed to delete has no
// future cleanup path, and [RunSteerAgent] surfaces the error so the dangling
// REDIRECT is observable.
func removeSteeringRule(
	ctx context.Context,
	redirect SteeringRedirect,
	runner SteerCommandRunner,
) error {
	deleteArgs, err := redirect.DeleteArgs()
	if err != nil {
		return fmt.Errorf("building the steering teardown rule: %w", err)
	}

	teardownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), steerTeardownTimeout)
	defer cancel()

	err = runner(teardownCtx, steerIptablesBinary, deleteArgs...)
	if err != nil {
		return fmt.Errorf("removing the steering redirect rule: %w", err)
	}

	return nil
}
