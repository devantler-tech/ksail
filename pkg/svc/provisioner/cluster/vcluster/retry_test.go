package vclusterprovisioner_test

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	vclusterprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/vcluster"
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
	errRegistryDenied = errors.New("fetching blob: denied: denied")
)

func newTestLogger() loftlog.Logger {
	return loftlog.NewStreamLogger(io.Discard, io.Discard, logrus.WarnLevel)
}

// --- isTransientCreateError tests ---

func TestIsTransientCreateError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "exit_status_22_is_transient",
			err:  errTransient,
			want: true,
		},
		{
			name: "permission_denied_is_not_transient",
			err:  errNonTransient,
			want: false,
		},
		{
			name: "dbus_error_is_not_transient",
			err:  errDBus,
			want: false,
		},
		{
			name: "exit_status_22_in_wrapped_error",
			err:  errWrapped22,
			want: true,
		},
		{
			name: "exit_status_1_is_not_transient",
			err:  errExitStatus1,
			want: false,
		},
		{
			name: "registry_denied_is_transient",
			err:  errRegistryDenied,
			want: true,
		},
		{
			name: "empty_error_is_not_transient",
			err:  errEmpty,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := vclusterprovisioner.IsTransientCreateErrorForTest(tt.err)
			assert.Equal(t, tt.want, got)
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
	require.ErrorContains(t, err, "failed to create vCluster after 3 attempts")
	require.ErrorIs(t, err, errTransient)
	assert.Equal(t, 3, createCalls, "create should be called maxAttempts times")
	assert.Equal(t, 2, cleanupCalls, "cleanup should be called before retries 2 and 3")
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
