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
// installs the iptables steering rules for redirect (the NAT REDIRECT plus the
// intercept-port guard that keeps the all-interfaces listener reachable only
// via that REDIRECT — #6039), accepts the redirected connections on listener
// and forwards each over the tunnel ([ForwardRedirected]), and — win or lose —
// removes the rules again before returning. Reversible teardown is a hard
// requirement of the #5839 design: an ephemeral container cannot be removed,
// so its rules must be, or the pod's traffic stays redirected to a dead agent.
//
// transport is the agent's byte pipe to the ksail side; when the agent runs as
// the ksail-steer container's process it is the exec channel
// [OpenExecTransport] opens, and tests pair it with the ksail side over an
// in-memory pipe. listener receives the connections the in-namespace REDIRECT
// delivers. runner installs and removes the rules (see [SteerCommandRunner]).
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
	listener net.Listener,
	redirect SteeringRedirect,
	runner SteerCommandRunner,
) (err error) {
	err = checkSteerAgentInputs(transport, listener, runner)
	if err != nil {
		return err
	}

	err = installSteeringRules(ctx, redirect, runner)
	if err != nil {
		return err
	}

	defer func() {
		teardownErr := removeSteeringRules(ctx, redirect, runner)
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

	return ForwardRedirected(ctx, listener, session)
}

// installSteeringRules installs the redirect and then its intercept-port
// guard. A guard-install failure rolls the already-installed redirect back
// before returning (joined with the rollback error if that fails too): a
// redirect without its guard would steer traffic to an unguarded
// all-interfaces listener, so the pair is installed atomically-or-not-at-all.
func installSteeringRules(
	ctx context.Context,
	redirect SteeringRedirect,
	runner SteerCommandRunner,
) error {
	insertArgs, err := redirect.InsertArgs()
	if err != nil {
		return fmt.Errorf("building the steering install rule: %w", err)
	}

	err = runner(ctx, steerIptablesBinary, insertArgs...)
	if err != nil {
		return fmt.Errorf("installing the steering redirect rule: %w", err)
	}

	guardArgs, err := redirect.GuardInsertArgs()
	if err == nil {
		err = runner(ctx, steerIptablesBinary, guardArgs...)
		if err != nil {
			err = fmt.Errorf("installing the intercept-port guard rule: %w", err)
		}
	} else {
		err = fmt.Errorf("building the intercept-port guard rule: %w", err)
	}

	if err != nil {
		return errors.Join(err, removeRule(ctx, redirect.DeleteArgs, runner, "steering redirect"))
	}

	return nil
}

// removeSteeringRules removes both steering rules in reverse install order —
// the guard first, the redirect last (first in, last out) — and attempts the
// second removal even when the first fails, joining the failures: each rule
// left dangling on an ephemeral pod must be observable.
func removeSteeringRules(
	ctx context.Context,
	redirect SteeringRedirect,
	runner SteerCommandRunner,
) error {
	return errors.Join(
		removeRule(ctx, redirect.GuardDeleteArgs, runner, "intercept-port guard"),
		removeRule(ctx, redirect.DeleteArgs, runner, "steering redirect"),
	)
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
