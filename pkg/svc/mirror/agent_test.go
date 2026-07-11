package mirror_test

import (
	"context"
	"errors"
	"io"
	"net"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/svc/mirror"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	errSteerInstallDenied  = errors.New("iptables: permission denied")
	errSteerGuardDenied    = errors.New("iptables: guard denied")
	errSteerTeardownDenied = errors.New("iptables: delete denied")
	errForwardBroken       = errors.New("accept: connection reset")
)

// failingListener is a net.Listener whose Accept fails immediately with a
// non-stop error, driving ForwardRedirected's genuine-failure path so a
// forwarding failure can be paired with a teardown failure in tests.
type failingListener struct{ err error }

func (l *failingListener) Accept() (net.Conn, error) { return nil, l.err }

func (l *failingListener) Close() error { return nil }

func (l *failingListener) Addr() net.Addr {
	return &net.UnixAddr{Name: "ksail-steer-failing", Net: "unix"}
}

// recordingRunner is a fake SteerCommandRunner: it records every command it is
// asked to run and can be told to fail whenever a given iptables action
// (`-I`/`-D`) appears in the args, standing in for the container's iptables
// without a live network namespace.
type recordingRunner struct {
	mu     sync.Mutex
	calls  [][]string
	failOn string
	err    error
}

func (r *recordingRunner) run(_ context.Context, name string, args ...string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.calls = append(r.calls, append([]string{name}, args...))

	if r.failOn != "" && slices.Contains(args, r.failOn) {
		return r.err
	}

	return nil
}

// sawAction reports whether any recorded call carried the iptables action
// (`-I` install / `-D` delete).
func (r *recordingRunner) sawAction(action string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, call := range r.calls {
		if slices.Contains(call, action) {
			return true
		}
	}

	return false
}

// callCount reports how many commands the runner has recorded so far.
func (r *recordingRunner) callCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()

	return len(r.calls)
}

// callHas reports whether the index-th recorded call carries every one of the
// given argument tokens — used to pin the install/teardown rule ordering.
func (r *recordingRunner) callHas(t *testing.T, index int, tokens ...string) bool {
	t.Helper()

	r.mu.Lock()
	defer r.mu.Unlock()

	if index >= len(r.calls) {
		t.Fatalf("no call at index %d, recorded %d: %v", index, len(r.calls), r.calls)
	}

	for _, token := range tokens {
		if !slices.Contains(r.calls[index], token) {
			return false
		}
	}

	return true
}

func listenerFactory(listener net.Listener) mirror.SteerListenerFactory {
	return func(context.Context, int) (net.Listener, error) {
		return listener, nil
	}
}

func TestRunSteerAgent_InstallsForwardsAndTearsDown(t *testing.T) {
	t.Parallel()

	runner := &recordingRunner{}
	agentTransport, ksailTransport := net.Pipe()
	listener := newPipeListener()
	redirect := mirror.SteeringRedirect{ServicePort: 8080, InterceptPort: 15006}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	agentDone := make(chan error, 1)

	go func() {
		agentDone <- mirror.RunSteerAgent(ctx, agentTransport, listenerFactory(listener), redirect, runner.run)
	}()

	// The ksail side: dial an echo "developer process" for each intercepted stream.
	client := mirror.NewTunnelSession(ksailTransport, ksailTransport, mirror.TunnelRoleClient)
	dial := func(_ context.Context) (io.ReadWriteCloser, error) {
		localApp, dialSide := net.Pipe()

		go func() { _, _ = io.Copy(localApp, localApp) }()

		return dialSide, nil
	}

	go func() { _ = mirror.ServeIntercepted(ctx, client, dial) }()

	// A redirected connection flows through the agent's tunnel to the echo process.
	clusterSide, redirected := net.Pipe()
	deadlined(t, clusterSide)
	listener.deliver(t, redirected)

	go func() { _, _ = clusterSide.Write([]byte("echo-me")) }()

	buffer := make([]byte, 7)
	_, err := io.ReadFull(clusterSide, buffer)
	require.NoError(t, err)
	assert.Equal(t, "echo-me", string(buffer))
	assert.True(t, runner.sawAction("-I"), "the redirect rule should be installed")

	// Cancelling stops the agent, which must remove the rule before returning.
	cancel()
	require.NoError(t, <-agentDone)
	assert.True(t, runner.sawAction("-D"), "the redirect rule should be torn down on exit")
}

func TestRunSteerAgent_GuardsTheInterceptPortForTheSessionLifetime(t *testing.T) {
	t.Parallel()

	// The listener binds all interfaces (#6039), so the agent must pair the
	// INPUT guard that drops direct or unrelated-DNAT hits on the intercept
	// port. The guard brackets both the all-interfaces listener and REDIRECT.
	runner := &recordingRunner{}
	agentTransport, _ := net.Pipe()
	listener := newPipeListener()
	redirect := mirror.SteeringRedirect{ServicePort: 8080, InterceptPort: 15006}

	ctx, cancel := context.WithCancel(context.Background())

	agentDone := make(chan error, 1)

	go func() {
		agentDone <- mirror.RunSteerAgent(ctx, agentTransport, listenerFactory(listener), redirect, runner.run)
	}()

	bothInstalled := func() bool { return runner.callCount() >= 2 }
	require.Eventually(t, bothInstalled, time.Second, time.Millisecond,
		"both steering rules should be installed")
	cancel()
	require.NoError(t, <-agentDone)

	require.Equal(t, 4, runner.callCount(), "expected redirect+guard installs and teardowns")
	assert.True(t, runner.callHas(t, 0, "-I", "DROP", "--dport", "15006"),
		"the guard installs before the listener opens")
	assert.True(t, runner.callHas(t, 1, "-I", "REDIRECT", "--dport", "8080"),
		"the redirect installs after the listener opens")
	assert.True(t, runner.callHas(t, 2, "-D", "REDIRECT", "--dport", "8080"),
		"the redirect is removed before the listener closes")
	assert.True(t, runner.callHas(t, 3, "-D", "DROP", "--dport", "15006"),
		"the guard is removed after the listener closes")
}

func TestRunSteerAgent_DoesNotOpenTheListenerWhenTheGuardInstallFails(t *testing.T) {
	t.Parallel()

	runner := &recordingRunner{failOn: "DROP", err: errSteerGuardDenied}
	agentTransport, _ := net.Pipe()
	redirect := mirror.SteeringRedirect{ServicePort: 8080, InterceptPort: 15006}
	listen := func(context.Context, int) (net.Listener, error) {
		t.Error("listener must not open when its guard failed to install")

		return nil, errSteerGuardDenied
	}

	err := mirror.RunSteerAgent(
		context.Background(),
		agentTransport,
		listen,
		redirect,
		runner.run,
	)

	require.ErrorIs(t, err, errSteerGuardDenied)
	require.Equal(t, 1, runner.callCount(), "only the guard install should be attempted")
	assert.False(t, runner.sawAction("-D"), "a failed guard install leaves nothing to remove")
}

func TestRunSteerAgent_AbortsWhenTheRuleInstallFails(t *testing.T) {
	t.Parallel()

	runner := &recordingRunner{failOn: "-I", err: errSteerInstallDenied}
	agentTransport, _ := net.Pipe()
	listener := newPipeListener()
	redirect := mirror.SteeringRedirect{ServicePort: 8080, InterceptPort: 15006}

	err := mirror.RunSteerAgent(
		context.Background(),
		agentTransport,
		listenerFactory(listener),
		redirect,
		runner.run,
	)

	require.ErrorIs(t, err, errSteerInstallDenied)
	assert.True(t, runner.sawAction("-I"), "install should have been attempted")
	assert.False(t, runner.sawAction("-D"), "nothing is installed, so nothing is torn down")
}

func TestRunSteerAgent_SurfacesTeardownFailure(t *testing.T) {
	t.Parallel()

	// The forwarding result is nil (ctx cancel), but the delete rule fails —
	// a dangling REDIRECT on an ephemeral pod must not vanish silently.
	runner := &recordingRunner{failOn: "-D", err: errSteerTeardownDenied}
	agentTransport, _ := net.Pipe()
	listener := newPipeListener()
	redirect := mirror.SteeringRedirect{ServicePort: 8080, InterceptPort: 15006}

	ctx, cancel := context.WithCancel(context.Background())

	agentDone := make(chan error, 1)

	go func() {
		agentDone <- mirror.RunSteerAgent(ctx, agentTransport, listenerFactory(listener), redirect, runner.run)
	}()

	installed := func() bool { return runner.sawAction("-I") }
	require.Eventually(t, installed, time.Second, time.Millisecond,
		"the redirect rule should be installed before teardown")
	cancel()

	require.ErrorIs(t, <-agentDone, errSteerTeardownDenied)
	assert.True(t, runner.sawAction("-D"), "teardown should have been attempted")
}

func TestRunSteerAgent_JoinsForwardingAndTeardownFailures(t *testing.T) {
	t.Parallel()

	// Forwarding fails (the listener's Accept errors with a non-stop error)
	// AND teardown fails: the dangling-rule teardown error must not be
	// swallowed by the forwarding error — both surface via errors.Join.
	runner := &recordingRunner{failOn: "-D", err: errSteerTeardownDenied}
	agentTransport, _ := net.Pipe()
	listener := &failingListener{err: errForwardBroken}
	redirect := mirror.SteeringRedirect{ServicePort: 8080, InterceptPort: 15006}

	err := mirror.RunSteerAgent(
		context.Background(),
		agentTransport,
		listenerFactory(listener),
		redirect,
		runner.run,
	)

	require.ErrorIs(t, err, errForwardBroken, "the forwarding failure must surface")
	require.ErrorIs(t, err, errSteerTeardownDenied, "the teardown failure must surface too")
	assert.True(t, runner.sawAction("-D"), "teardown should have been attempted")
}

func TestRunSteerAgent_RejectsAnInvalidRedirect(t *testing.T) {
	t.Parallel()

	runner := &recordingRunner{}
	agentTransport, _ := net.Pipe()
	listener := newPipeListener()
	redirect := mirror.SteeringRedirect{ServicePort: 0, InterceptPort: 15006}

	err := mirror.RunSteerAgent(
		context.Background(),
		agentTransport,
		listenerFactory(listener),
		redirect,
		runner.run,
	)

	require.ErrorIs(t, err, mirror.ErrSteeringPortInvalid)
	assert.Empty(t, runner.calls, "an invalid redirect never reaches iptables")
}

func TestRunSteerAgent_RejectsNilDependencies(t *testing.T) {
	t.Parallel()

	agentTransport, _ := net.Pipe()
	listener := newPipeListener()
	redirect := mirror.SteeringRedirect{ServicePort: 8080, InterceptPort: 15006}
	runner := (&recordingRunner{}).run

	tests := map[string]struct {
		transport io.ReadWriteCloser
		listen    mirror.SteerListenerFactory
		runner    mirror.SteerCommandRunner
		wantErr   error
	}{
		"nil transport": {nil, listenerFactory(listener), runner, mirror.ErrSteerTransportNil},
		"nil listener":  {agentTransport, nil, runner, mirror.ErrSteerListenerNil},
		"nil runner":    {agentTransport, listenerFactory(listener), nil, mirror.ErrSteerRunnerNil},
	}

	for name, testCase := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := mirror.RunSteerAgent(
				context.Background(),
				testCase.transport,
				testCase.listen,
				redirect,
				testCase.runner,
			)

			require.ErrorIs(t, err, testCase.wantErr)
		})
	}
}
