package mirror_test

import (
	"context"
	"errors"
	"io"
	"net"
	"slices"
	"sync"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/mirror"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errSteerInstallDenied = errors.New("iptables: permission denied")

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
		agentDone <- mirror.RunSteerAgent(ctx, agentTransport, listener, redirect, runner.run)
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

func TestRunSteerAgent_AbortsWhenTheRuleInstallFails(t *testing.T) {
	t.Parallel()

	runner := &recordingRunner{failOn: "-I", err: errSteerInstallDenied}
	agentTransport, _ := net.Pipe()
	listener := newPipeListener()
	redirect := mirror.SteeringRedirect{ServicePort: 8080, InterceptPort: 15006}

	err := mirror.RunSteerAgent(
		context.Background(),
		agentTransport,
		listener,
		redirect,
		runner.run,
	)

	require.ErrorIs(t, err, errSteerInstallDenied)
	assert.True(t, runner.sawAction("-I"), "install should have been attempted")
	assert.False(t, runner.sawAction("-D"), "nothing is installed, so nothing is torn down")
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
		listener,
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
		listener  net.Listener
		runner    mirror.SteerCommandRunner
		wantErr   error
	}{
		"nil transport": {nil, listener, runner, mirror.ErrSteerTransportNil},
		"nil listener":  {agentTransport, nil, runner, mirror.ErrSteerListenerNil},
		"nil runner":    {agentTransport, listener, nil, mirror.ErrSteerRunnerNil},
	}

	for name, testCase := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := mirror.RunSteerAgent(
				context.Background(),
				testCase.transport,
				testCase.listener,
				redirect,
				testCase.runner,
			)

			require.ErrorIs(t, err, testCase.wantErr)
		})
	}
}
