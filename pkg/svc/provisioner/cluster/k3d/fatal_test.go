package k3dprovisioner_test

import (
	"errors"
	"io"
	"sync"
	"testing"

	k3dprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/k3d"
	v1alpha5 "github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	k3dlogger "github.com/k3d-io/k3d/v5/pkg/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	// errFatalNotRaised is returned by a test action whose k3d fatal call did not
	// stop it, which would mean the fatal exit was not intercepted.
	errFatalNotRaised = errors.New("k3d fatal call returned instead of stopping the command")

	// errDaemonUnreachable is what the runtime check reports when Docker is down.
	errDaemonUnreachable = errors.New(
		"docker failed to provide info output: Cannot connect to the Docker daemon at " +
			"unix:///var/run/docker.sock. Is the docker daemon running?",
	)
)

// muteK3dLogger silences k3d's own logger for the test, so the fatal lines the
// tests raise do not clutter the test log.
func muteK3dLogger(t *testing.T) {
	t.Helper()

	k3dLog := k3dlogger.Log()
	originalOut := k3dLog.Out
	k3dLog.SetOutput(io.Discard)

	t.Cleanup(func() { k3dLog.SetOutput(originalOut) })
}

// raiseK3dFatal returns an action that fails the way k3d's commands do: by logging
// message at fatal level on k3d's logger.
func raiseK3dFatal(message string) func() error {
	return func() error {
		k3dlogger.Log().Fatalln(message)

		return errFatalNotRaised
	}
}

// newProvisionerWithRuntime returns a provisioner whose runtime check gives the
// chosen answer: nil for a reachable Docker daemon, an error for an unreachable one.
func newProvisionerWithRuntime(probe func() error) *k3dprovisioner.Provisioner {
	return k3dprovisioner.NewProvisioner(&v1alpha5.SimpleConfig{}, "").
		WithRuntimeProbeForTest(probe)
}

func reachableRuntime() error { return nil }

// TestRunK3dSafely_FatalErrorKeepsK3dReason reproduces #8356: k3d rolled a cluster
// back because a helper node was not ready in time, while Docker was running.
// The error must carry k3d's reason and must not send the user to check Docker.
//
//nolint:paralleltest // serial: these tests share the process-global k3d logger
func TestRunK3dSafely_FatalErrorKeepsK3dReason(t *testing.T) {
	muteK3dLogger(t)

	const reason = "Failed Cluster Start: Failed to add one or more helper nodes: " +
		"Node k3d-calico-serverlb failed to get ready: error waiting for log line " +
		"`start worker processes` from node 'k3d-calico-serverlb': stopped returning log lines"

	prov := newProvisionerWithRuntime(reachableRuntime)

	err := prov.RunK3dSafelyForTest(raiseK3dFatal(reason))

	require.ErrorIs(t, err, k3dprovisioner.ErrK3dCommandFailed)
	require.NotErrorIs(t, err, k3dprovisioner.ErrK3dRuntimeUnavailable)
	assert.Contains(t, err.Error(), reason)
	assert.NotContains(t, err.Error(), "Docker daemon running")
}

// TestRunK3dSafely_UnreachableDaemonMessageKeepsDockerHint checks that a fatal
// message that already shows an unreachable Docker daemon is reported as an
// unavailable runtime, with the Docker hint and the daemon address, without
// checking the runtime again.
//
//nolint:paralleltest // serial: these tests share the process-global k3d logger
func TestRunK3dSafely_UnreachableDaemonMessageKeepsDockerHint(t *testing.T) {
	muteK3dLogger(t)

	const reason = "Failed to get nodes for cluster 'dev': docker failed to get containers: " +
		"Cannot connect to the Docker daemon at unix:///var/run/docker.sock. " +
		"Is the docker daemon running?"

	prov := newProvisionerWithRuntime(func() error {
		t.Error("the runtime must not be checked again when the message already shows it is down")

		return nil
	})

	err := prov.RunK3dSafelyForTest(raiseK3dFatal(reason))

	require.ErrorIs(t, err, k3dprovisioner.ErrK3dRuntimeUnavailable)
	require.NotErrorIs(t, err, k3dprovisioner.ErrK3dCommandFailed)
	assert.Contains(t, err.Error(), "is the Docker daemon running?")
	assert.Contains(t, err.Error(), "unix:///var/run/docker.sock")
}

// TestRunK3dSafely_GenericFatalWithDockerDownKeepsDockerHint covers k3d's usual
// Docker-down exit: it logs the runtime error at a lower level, then exits with a
// generic message. The runtime check finds Docker down, so the Docker hint stays,
// alongside k3d's message and the reason the check failed.
//
//nolint:paralleltest // serial: these tests share the process-global k3d logger
func TestRunK3dSafely_GenericFatalWithDockerDownKeepsDockerHint(t *testing.T) {
	muteK3dLogger(t)

	const reason = "No nodes found for given cluster"

	prov := newProvisionerWithRuntime(func() error { return errDaemonUnreachable })

	err := prov.RunK3dSafelyForTest(raiseK3dFatal(reason))

	require.ErrorIs(t, err, k3dprovisioner.ErrK3dRuntimeUnavailable)
	require.ErrorIs(t, err, errDaemonUnreachable)
	require.NotErrorIs(t, err, k3dprovisioner.ErrK3dCommandFailed)
	assert.Contains(t, err.Error(), "is the Docker daemon running?")
	assert.Contains(t, err.Error(), reason)
}

// TestRunK3dSafely_SilentExitIsNotBlamedOnRunningDocker checks the backstop: an
// exit that logs no fatal message still becomes an error rather than ending the
// process, and is not blamed on Docker while Docker answers.
//
//nolint:paralleltest // serial: these tests share the process-global k3d logger
func TestRunK3dSafely_SilentExitIsNotBlamedOnRunningDocker(t *testing.T) {
	prov := newProvisionerWithRuntime(reachableRuntime)

	err := prov.RunK3dSafelyForTest(func() error {
		k3dlogger.Log().Exit(1)

		return errFatalNotRaised
	})

	require.ErrorIs(t, err, k3dprovisioner.ErrK3dCommandFailed)
	require.NotErrorIs(t, err, k3dprovisioner.ErrK3dRuntimeUnavailable)
}

// TestRunK3dSafely_ConcurrentFatalErrorsKeepTheirOwnReason checks that commands
// failing at the same time on the shared k3d logger each get their own message.
//
//nolint:paralleltest // serial: these tests share the process-global k3d logger
func TestRunK3dSafely_ConcurrentFatalErrorsKeepTheirOwnReason(t *testing.T) {
	muteK3dLogger(t)

	prov := newProvisionerWithRuntime(reachableRuntime)

	reasons := []string{
		"Failed Cluster Creation: first reason",
		"Failed Cluster Creation: second reason",
		"Failed Cluster Creation: third reason",
		"Failed Cluster Creation: fourth reason",
	}
	errs := make([]error, len(reasons))

	var waitGroup sync.WaitGroup

	for index, reason := range reasons {
		waitGroup.Go(func() {
			errs[index] = prov.RunK3dSafelyForTest(raiseK3dFatal(reason))
		})
	}

	waitGroup.Wait()

	for index, reason := range reasons {
		require.ErrorIs(t, errs[index], k3dprovisioner.ErrK3dCommandFailed)
		assert.Contains(t, errs[index].Error(), reason)

		for other, otherReason := range reasons {
			if other != index {
				assert.NotContains(t, errs[index].Error(), otherReason)
			}
		}
	}
}

// TestRunK3dSafely_OtherPanicIsReRaised checks that a panic that is not k3d's
// fatal exit is a genuine bug and is not swallowed.
func TestRunK3dSafely_OtherPanicIsReRaised(t *testing.T) {
	t.Parallel()

	prov := newProvisionerWithRuntime(reachableRuntime)

	assert.PanicsWithValue(t, "unrelated bug", func() {
		_ = prov.RunK3dSafelyForTest(func() error {
			panic("unrelated bug")
		})
	})
}
