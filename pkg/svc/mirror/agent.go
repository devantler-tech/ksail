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
// listener factory for the redirected connections.
var ErrSteerListenerNil = errors.New("steering listener factory must not be nil")

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

// SteerListenerFactory opens the all-interfaces listener after the guard is in
// place. Keeping the bind behind this seam lets RunSteerAgent guarantee that no
// reachable listener exists before its fail-closed INPUT rule.
type SteerListenerFactory func(ctx context.Context, port int) (net.Listener, error)

// RunSteerAgent is the steering agent's in-container entrypoint composition: it
// installs the intercept-port guard, opens the all-interfaces listener, then
// installs the NAT REDIRECT. It accepts redirected connections and forwards
// each over the tunnel ([ForwardRedirected]), then tears the resources down in
// reverse: REDIRECT, listener, guard. This ordering means the listener is never
// reachable without its guard (#6039). Reversible teardown is a hard
// requirement of the #5839 design: an ephemeral container cannot be removed,
// so its rules must be, or the pod's traffic stays redirected to a dead agent.
//
// transport is the agent's byte pipe to the ksail side; when the agent runs as
// the ksail-steer container's process it is the exec channel
// [OpenExecTransport] opens, and tests pair it with the ksail side over an
// in-memory pipe. listen opens the listener that receives connections from the
// in-namespace REDIRECT. runner installs and removes the rules (see
// [SteerCommandRunner]).
//
// It blocks until ctx is cancelled or the tunnel session ends (both return
// nil, matching [ForwardRedirected]) or the listener fails (returns the error).
// The rule teardown runs on a context detached from ctx and time-bounded, so a
// cancelled agent still cleans up; a teardown failure is joined onto any
// forwarding error (via [errors.Join]) and surfaced as the returned error, so a
// dangling steering rule on an ephemeral pod is observable rather than silent —
// even when forwarding failed too.
func RunSteerAgent(
	ctx context.Context,
	transport io.ReadWriteCloser,
	listen SteerListenerFactory,
	redirect SteeringRedirect,
	runner SteerCommandRunner,
) (err error) {
	err = checkSteerAgentInputs(transport, listen, runner)
	if err != nil {
		return err
	}

	err = redirect.Validate()
	if err != nil {
		return fmt.Errorf("validating the steering redirect: %w", err)
	}

	err = installRule(ctx, redirect.GuardInsertArgs, runner, "intercept-port guard")
	if err != nil {
		return err
	}

	defer func() {
		teardownErr := removeRule(ctx, redirect.GuardDeleteArgs, runner, "intercept-port guard")
		if teardownErr != nil {
			err = errors.Join(err, teardownErr)
		}
	}()

	listener, err := listen(ctx, redirect.InterceptPort)
	if err != nil {
		return fmt.Errorf(
			"opening the steering listener on port %d: %w",
			redirect.InterceptPort,
			err,
		)
	}

	if listener == nil {
		return ErrSteerListenerNil
	}

	defer func() { _ = listener.Close() }()

	err = installRule(ctx, redirect.InsertArgs, runner, "steering redirect")
	if err != nil {
		return err
	}

	defer func() {
		teardownErr := removeRule(ctx, redirect.DeleteArgs, runner, "steering redirect")
		if teardownErr != nil {
			// Join rather than only surfacing teardown when forwarding
			// succeeded: a teardown failure that coincides with a forwarding
			// failure still leaves a dangling steering rule on an ephemeral
			// pod, so both must be observable, not just the first.
			err = errors.Join(err, teardownErr)
		}
	}()

	session := NewTunnelSession(transport, transport, TunnelRoleServer)

	defer func() { _ = session.Close() }()

	// Self-terminate when the client goes silent: the exec stream does not
	// reliably deliver EOF on unclean client death (ksail#6040), and an
	// ephemeral container cannot be removed from outside, so this liveness
	// deadline is the only path that removes the REDIRECT rule once the
	// client is gone. Cancelling forwardCtx ends ForwardRedirected, which
	// returns into the reverse teardown above.
	forwardCtx, expire := context.WithCancel(ctx)
	defer expire()

	go watchSessionLiveness(forwardCtx, session, SteerClientLivenessTimeout, expire)

	return ForwardRedirected(forwardCtx, listener, session)
}

// installRule builds and installs one steering rule.
func installRule(
	ctx context.Context,
	buildArgs func() ([]string, error),
	runner SteerCommandRunner,
	ruleName string,
) error {
	insertArgs, err := buildArgs()
	if err != nil {
		return fmt.Errorf("building the %s install rule: %w", ruleName, err)
	}

	err = runner(ctx, steerIptablesBinary, insertArgs...)
	if err != nil {
		return fmt.Errorf("installing the %s rule: %w", ruleName, err)
	}

	return nil
}

// checkSteerAgentInputs rejects the nil dependencies that would otherwise
// panic once the agent starts forwarding, so a misuse fails with a clear error
// instead of a crash mid-stream.
func checkSteerAgentInputs(
	transport io.ReadWriteCloser,
	listen SteerListenerFactory,
	runner SteerCommandRunner,
) error {
	if transport == nil {
		return ErrSteerTransportNil
	}

	if listen == nil {
		return ErrSteerListenerNil
	}

	if runner == nil {
		return ErrSteerRunnerNil
	}

	return nil
}

// removeRule runs one rule's delete vector on a context detached from the
// agent's own — the agent stops precisely because ctx was cancelled, so
// reusing it would kill the teardown command before it could undo the rule —
// and time-bounded, so a wedged iptables cannot hang teardown forever. It
// returns the build-or-run failure rather than swallowing it: an ephemeral
// container cannot be removed, so a rule the container failed to delete has no
// future cleanup path, and the callers surface the error so the dangling rule
// is observable.
func removeRule(
	ctx context.Context,
	buildArgs func() ([]string, error),
	runner SteerCommandRunner,
	ruleName string,
) error {
	deleteArgs, err := buildArgs()
	if err != nil {
		return fmt.Errorf("building the %s teardown rule: %w", ruleName, err)
	}

	teardownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), steerTeardownTimeout)
	defer cancel()

	err = runner(teardownCtx, steerIptablesBinary, deleteArgs...)
	if err != nil {
		return fmt.Errorf("removing the %s rule: %w", ruleName, err)
	}

	return nil
}
