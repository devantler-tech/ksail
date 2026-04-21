package vclusterprovisioner_test

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	vclusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/vcluster"
	loftlog "github.com/loft-sh/log"
	"github.com/loft-sh/vcluster/pkg/cli"
	"github.com/loft-sh/vcluster/pkg/cli/flags"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	errTransient    = errors.New("failed to start vCluster standalone: exit status 22")
	errNonTransient = errors.New("failed to start vCluster standalone: permission denied")
	//nolint:staticcheck // Must match the exact SDK error string which is capitalised.
	errDBus           = errors.New("Failed to connect to bus: No such file or directory")
	errDBusRecover    = errors.New("D-Bus recovery failed")
	errExitStatus1    = errors.New("exit status 1")
	errWrapped22      = errors.New("something went wrong: exit status 22")
	errEmpty          = errors.New("")
	errRegistryDenied = errors.New(
		"reading blob sha256:abc: fetching blob: denied: denied",
	)
	errEgressLimit = errors.New(
		"copying blob sha256:fa365: fetching blob: received unexpected HTTP status: 503 Egress is over the account limit",
	)
	errIOTimeout      = errors.New("dial tcp 1.2.3.4:443: i/o timeout")
	errConnReset      = errors.New("read tcp 10.0.0.1:54321->1.2.3.4:443: connection reset by peer")
	errTLSTimeout     = errors.New("net/http: TLS handshake timeout")
	errNoSuchHost     = errors.New("dial tcp: lookup ghcr.io: no such host")
	errDNSTransient   = errors.New("dial tcp: lookup ghcr.io: temporary failure in name resolution")
	errNodeJoinFailed = errors.New(
		"failed to start vCluster standalone. Node couldn't join: signal: killed",
	)
)

func newTestLogger() loftlog.Logger {
	return loftlog.NewStreamLogger(io.Discard, io.Discard, logrus.WarnLevel)
}

// --- isTransientCreateError tests ---

func TestIsTransientCreateError(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		err  error
		want bool
	}{
		{"exit_status_22_is_transient", errTransient, true},
		{"permission_denied_is_not_transient", errNonTransient, false},
		{"dbus_error_is_not_transient", errDBus, false},
		{"exit_status_22_in_wrapped_error", errWrapped22, true},
		{"exit_status_1_is_not_transient", errExitStatus1, false},
		{"registry_denied_is_transient", errRegistryDenied, true},
		{"egress_limit_is_transient", errEgressLimit, true},
		{"io_timeout_is_transient", errIOTimeout, true},
		{"connection_reset_is_transient", errConnReset, true},
		{"tls_handshake_timeout_is_transient", errTLSTimeout, true},
		{"no_such_host_is_transient", errNoSuchHost, true},
		{"dns_temporary_failure_is_transient", errDNSTransient, true},
		{"node_join_failed_is_transient", errNodeJoinFailed, true},
		{"empty_error_is_not_transient", errEmpty, false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := vclusterprovisioner.IsTransientCreateErrorForTest(testCase.err)
			assert.Equal(t, testCase.want, got)
		})
	}
}

// --- createWithRetry tests ---

func TestCreateWithRetry_Success(t *testing.T) {
	t.Parallel()

	createCalls := 0
	create := func(_ context.Context, _ *cli.CreateOptions, _ *flags.GlobalFlags, _ string, _ loftlog.Logger) error {
		createCalls++

		return nil
	}

	cleanup := func(_ context.Context, _ *flags.GlobalFlags, _ string, _ loftlog.Logger) {
		t.Error("cleanup should not be called on success")
	}

	recoverDBus := func(_ context.Context, _ *flags.GlobalFlags, _ string, _ loftlog.Logger) error {
		t.Error("D-Bus recovery should not be called on success")

		return nil
	}

	err := vclusterprovisioner.CreateWithRetryForTest(
		context.Background(),
		&cli.CreateOptions{},
		&flags.GlobalFlags{},
		"test-cluster",
		newTestLogger(),
		time.Millisecond,
		create, cleanup, recoverDBus,
	)

	require.NoError(t, err)
	assert.Equal(t, 1, createCalls, "create should be called exactly once")
}

func TestCreateWithRetry_TransientErrorRetries(t *testing.T) {
	t.Parallel()

	createCalls := 0
	create := func(_ context.Context, _ *cli.CreateOptions, _ *flags.GlobalFlags, _ string, _ loftlog.Logger) error {
		createCalls++
		if createCalls < 3 {
			return errTransient
		}

		return nil
	}

	cleanupCalls := 0
	cleanup := func(_ context.Context, _ *flags.GlobalFlags, _ string, _ loftlog.Logger) {
		cleanupCalls++
	}

	recoverDBus := func(_ context.Context, _ *flags.GlobalFlags, _ string, _ loftlog.Logger) error {
		t.Error("D-Bus recovery should not be called for transient errors")

		return nil
	}

	err := vclusterprovisioner.CreateWithRetryForTest(
		context.Background(),
		&cli.CreateOptions{},
		&flags.GlobalFlags{},
		"test-cluster",
		newTestLogger(),
		time.Millisecond,
		create, cleanup, recoverDBus,
	)

	require.NoError(t, err)
	assert.Equal(t, 3, createCalls, "create should be called 3 times")
	assert.Equal(t, 2, cleanupCalls, "cleanup should be called before each retry")
}

func TestCreateWithRetry_TransientErrorExhaustsAttempts(t *testing.T) {
	t.Parallel()

	createCalls := 0
	create := func(_ context.Context, _ *cli.CreateOptions, _ *flags.GlobalFlags, _ string, _ loftlog.Logger) error {
		createCalls++

		return errTransient
	}

	cleanupCalls := 0
	cleanup := func(_ context.Context, _ *flags.GlobalFlags, _ string, _ loftlog.Logger) {
		cleanupCalls++
	}

	recoverDBus := func(_ context.Context, _ *flags.GlobalFlags, _ string, _ loftlog.Logger) error {
		return nil
	}

	err := vclusterprovisioner.CreateWithRetryForTest(
		context.Background(),
		&cli.CreateOptions{},
		&flags.GlobalFlags{},
		"test-cluster",
		newTestLogger(),
		time.Millisecond,
		create, cleanup, recoverDBus,
	)

	require.Error(t, err)
	require.ErrorContains(t, err, "failed to create vCluster after 5 attempts")
	require.ErrorIs(t, err, errTransient)
	assert.Equal(t, 5, createCalls, "create should be called maxAttempts times")
	assert.Equal(t, 4, cleanupCalls, "cleanup should be called before retries 2 through 5")
}

func TestCreateWithRetry_NonTransientErrorFailsImmediately(t *testing.T) {
	t.Parallel()

	createCalls := 0
	create := func(_ context.Context, _ *cli.CreateOptions, _ *flags.GlobalFlags, _ string, _ loftlog.Logger) error {
		createCalls++

		return errNonTransient
	}

	cleanup := func(_ context.Context, _ *flags.GlobalFlags, _ string, _ loftlog.Logger) {
		t.Error("cleanup should not be called for non-transient errors")
	}

	recoverDBus := func(_ context.Context, _ *flags.GlobalFlags, _ string, _ loftlog.Logger) error {
		t.Error("D-Bus recovery should not be called for non-transient errors")

		return nil
	}

	err := vclusterprovisioner.CreateWithRetryForTest(
		context.Background(),
		&cli.CreateOptions{},
		&flags.GlobalFlags{},
		"test-cluster",
		newTestLogger(),
		time.Millisecond,
		create, cleanup, recoverDBus,
	)

	require.Error(t, err)
	require.ErrorContains(t, err, "failed to create vCluster")
	require.ErrorIs(t, err, errNonTransient)
	assert.Equal(t, 1, createCalls, "create should be called exactly once")
}

func TestCreateWithRetry_DBusErrorTriggersRecovery(t *testing.T) {
	t.Parallel()

	create := func(_ context.Context, _ *cli.CreateOptions, _ *flags.GlobalFlags, _ string, _ loftlog.Logger) error {
		return errDBus
	}

	cleanup := func(_ context.Context, _ *flags.GlobalFlags, _ string, _ loftlog.Logger) {
		t.Error("cleanup should not be called for D-Bus errors")
	}

	recoverCalls := 0
	recoverDBus := func(_ context.Context, _ *flags.GlobalFlags, _ string, _ loftlog.Logger) error {
		recoverCalls++

		return nil
	}

	err := vclusterprovisioner.CreateWithRetryForTest(
		context.Background(),
		&cli.CreateOptions{},
		&flags.GlobalFlags{},
		"test-cluster",
		newTestLogger(),
		time.Millisecond,
		create, cleanup, recoverDBus,
	)

	require.NoError(t, err)
	assert.Equal(t, 1, recoverCalls, "D-Bus recovery should be called once")
}

func TestCreateWithRetry_DBusRecoveryFailure(t *testing.T) {
	t.Parallel()

	create := func(_ context.Context, _ *cli.CreateOptions, _ *flags.GlobalFlags, _ string, _ loftlog.Logger) error {
		return errDBus
	}

	cleanup := func(_ context.Context, _ *flags.GlobalFlags, _ string, _ loftlog.Logger) {}

	recoverDBus := func(_ context.Context, _ *flags.GlobalFlags, _ string, _ loftlog.Logger) error {
		return errDBusRecover
	}

	err := vclusterprovisioner.CreateWithRetryForTest(
		context.Background(),
		&cli.CreateOptions{},
		&flags.GlobalFlags{},
		"test-cluster",
		newTestLogger(),
		time.Millisecond,
		create, cleanup, recoverDBus,
	)

	require.Error(t, err)
	require.ErrorContains(t, err, "D-Bus recovery failed")
	require.ErrorIs(t, err, errDBusRecover)
}

func TestCreateWithRetry_ContextCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	createCalls := 0
	create := func(_ context.Context, _ *cli.CreateOptions, _ *flags.GlobalFlags, _ string, _ loftlog.Logger) error {
		createCalls++

		cancel()

		return errTransient
	}

	cleanupCalls := 0
	cleanup := func(_ context.Context, _ *flags.GlobalFlags, _ string, _ loftlog.Logger) {
		cleanupCalls++
	}

	recoverDBus := func(_ context.Context, _ *flags.GlobalFlags, _ string, _ loftlog.Logger) error {
		return nil
	}

	err := vclusterprovisioner.CreateWithRetryForTest(
		ctx,
		&cli.CreateOptions{},
		&flags.GlobalFlags{},
		"test-cluster",
		newTestLogger(),
		time.Second,
		create, cleanup, recoverDBus,
	)

	require.Error(t, err)
	require.ErrorContains(t, err, "context cancelled during create retry")
	assert.Equal(t, 1, createCalls, "create should be called once before context is cancelled")
	assert.Equal(t, 1, cleanupCalls, "cleanup should be called before the cancelled retry delay")
}

func TestCreateWithRetry_CleanupErrorDoesNotPropagate(t *testing.T) {
	t.Parallel()

	createCalls := 0
	create := func(_ context.Context, _ *cli.CreateOptions, _ *flags.GlobalFlags, _ string, _ loftlog.Logger) error {
		createCalls++
		if createCalls == 1 {
			return errTransient
		}

		return nil
	}

	cleanup := func(_ context.Context, _ *flags.GlobalFlags, _ string, _ loftlog.Logger) {
		// Cleanup "fails" internally but doesn't return errors
		// (matching cleanupFailedCreate behavior that logs-and-swallows).
		// The retry should still proceed successfully.
	}

	recoverDBus := func(_ context.Context, _ *flags.GlobalFlags, _ string, _ loftlog.Logger) error {
		return nil
	}

	err := vclusterprovisioner.CreateWithRetryForTest(
		context.Background(),
		&cli.CreateOptions{},
		&flags.GlobalFlags{},
		"test-cluster",
		newTestLogger(),
		time.Millisecond,
		create, cleanup, recoverDBus,
	)

	require.NoError(t, err, "cleanup errors should not prevent successful retry")
	assert.Equal(t, 2, createCalls)
}

// --- waitForNetworkRemoval tests ---

func TestWaitForNetworkRemoval_NetworkAlreadyGone(t *testing.T) {
	t.Parallel()

	existsCalls := 0
	networkExists := func(_ context.Context, _ string) bool {
		existsCalls++

		return false
	}

	removeNetwork := func(_ context.Context, _ string, _ loftlog.Logger) {
		t.Error("removeNetwork should not be called when network does not exist")
	}

	vclusterprovisioner.WaitForNetworkRemovalForTest(
		context.Background(),
		"test-cluster",
		newTestLogger(),
		networkExists,
		removeNetwork,
		vclusterprovisioner.TestPollInterval,
	)

	assert.Equal(t, 1, existsCalls, "should check existence once and return immediately")
}

func TestWaitForNetworkRemoval_RemovedImmediately(t *testing.T) {
	t.Parallel()

	existsCalls := 0
	networkExists := func(_ context.Context, _ string) bool {
		existsCalls++

		// First call: network exists; second call (after rm): gone.
		return existsCalls <= 1
	}

	removeCalls := 0
	removeNetwork := func(_ context.Context, _ string, _ loftlog.Logger) {
		removeCalls++
	}

	vclusterprovisioner.WaitForNetworkRemovalForTest(
		context.Background(),
		"test-cluster",
		newTestLogger(),
		networkExists,
		removeNetwork,
		vclusterprovisioner.TestPollInterval,
	)

	assert.Equal(t, 2, existsCalls, "should check before and after removal")
	assert.Equal(t, 1, removeCalls, "should attempt removal once")
}

func TestWaitForNetworkRemoval_LingeringNetworkDisappearsAfterRetries(t *testing.T) {
	t.Parallel()

	existsCalls := 0
	networkExists := func(_ context.Context, _ string) bool {
		existsCalls++

		// Disappears on the 4th existence check (initial + 2 post-remove retries + final).
		return existsCalls <= 3
	}

	removeCalls := 0
	removeNetwork := func(_ context.Context, _ string, _ loftlog.Logger) {
		removeCalls++
	}

	vclusterprovisioner.WaitForNetworkRemovalForTest(
		context.Background(),
		"test-cluster",
		newTestLogger(),
		networkExists,
		removeNetwork,
		vclusterprovisioner.TestPollInterval,
	)

	assert.Equal(t, 4, existsCalls, "should check until network disappears")
	assert.Equal(t, 3, removeCalls, "should attempt removal once per loop iteration")
}

func TestWaitForNetworkRemoval_ContextCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	existsCalls := 0
	networkExists := func(_ context.Context, _ string) bool {
		existsCalls++
		if existsCalls == 2 {
			cancel()
		}

		return true // always exists
	}

	removeNetwork := func(_ context.Context, _ string, _ loftlog.Logger) {}

	vclusterprovisioner.WaitForNetworkRemovalForTest(
		ctx,
		"test-cluster",
		newTestLogger(),
		networkExists,
		removeNetwork,
		vclusterprovisioner.TestPollInterval,
	)

	// Should exit due to context cancellation, not loop forever.
	assert.GreaterOrEqual(t, existsCalls, 2, "should have checked existence before cancellation")
}
