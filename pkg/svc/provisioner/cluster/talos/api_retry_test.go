package talosprovisioner_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Near-zero backoff so retry tests complete instantly.
const (
	testRetryMaxAttempts = 3
	testRetryBaseWait    = time.Millisecond
	testRetryMaxWait     = 2 * time.Millisecond
)

var (
	errUnavailableMessage = errors.New(
		`rpc error: code = Unavailable desc = connection error: desc = ` +
			`"transport: authentication handshake failed: context deadline exceeded"`,
	)
	errHandshakeFailed = errors.New(
		"transport: authentication handshake failed: context deadline exceeded",
	)
	errDeadlineExceededMessage = errors.New(
		"rpc error: code = DeadlineExceeded desc = context deadline exceeded",
	)
	errPermissionDenied = errors.New("permission denied")
)

func TestIsTransientTalosAPIError(t *testing.T) { //nolint:funlen // table-driven tests
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "NilError",
			err:  nil,
			want: false,
		},
		{
			name: "GRPCUnavailable",
			err:  status.Error(codes.Unavailable, "connection error"),
			want: true,
		},
		{
			name: "GRPCDeadlineExceeded",
			err:  status.Error(codes.DeadlineExceeded, "context deadline exceeded"),
			want: true,
		},
		{
			name: "WrappedGRPCUnavailable",
			err: fmt.Errorf(
				"failed to apply configuration: %w",
				status.Error(codes.Unavailable, "connection error"),
			),
			want: true,
		},
		{
			name: "WrappedGRPCDeadlineExceeded",
			err: fmt.Errorf(
				"failed to fetch machine config from 1.2.3.4: %w",
				status.Error(codes.DeadlineExceeded, "context deadline exceeded"),
			),
			want: true,
		},
		{
			name: "UnavailableMessage",
			err:  errUnavailableMessage,
			want: true,
		},
		{
			name: "DeadlineExceededMessage",
			err:  errDeadlineExceededMessage,
			want: true,
		},
		{
			name: "HandshakeFailedMessage",
			err:  errHandshakeFailed,
			want: true,
		},
		{
			name: "ContextDeadlineExceeded",
			err:  context.DeadlineExceeded,
			want: true,
		},
		{
			name: "NonRetryableGrpcError",
			err:  status.Error(codes.InvalidArgument, "invalid request"),
			want: false,
		},
		{
			name: "NonRetryableError",
			err:  errPermissionDenied,
			want: false,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := talosprovisioner.IsTransientTalosAPIError(testCase.err)
			if got != testCase.want {
				t.Fatalf("IsTransientTalosAPIError() = %v, want %v", got, testCase.want)
			}
		})
	}
}

// newRetryTestProvisioner returns a provisioner with near-zero retry delays
// and its captured log output.
func newRetryTestProvisioner() (*talosprovisioner.Provisioner, *bytes.Buffer) {
	logBuf := &bytes.Buffer{}
	provisioner := talosprovisioner.NewProvisioner(nil, nil).
		WithLogWriter(logBuf).
		WithTalosAPIRetryConfig(testRetryMaxAttempts, testRetryBaseWait, testRetryMaxWait)

	return provisioner, logBuf
}

func TestRetryTransientTalosAPICall_SucceedsFirstAttempt(t *testing.T) {
	t.Parallel()

	provisioner, logBuf := newRetryTestProvisioner()
	calls := 0

	err := provisioner.RetryTransientTalosAPICallForTest(
		t.Context(), "1.2.3.4", "Config apply",
		func(_ context.Context) error {
			calls++

			return nil
		})

	require.NoError(t, err)
	assert.Equal(t, 1, calls)
	assert.Empty(t, logBuf.String())
}

func TestRetryTransientTalosAPICall_SucceedsAfterTransientFailures(t *testing.T) {
	t.Parallel()

	provisioner, logBuf := newRetryTestProvisioner()
	transientErr := status.Error(codes.Unavailable, "authentication handshake failed")
	calls := 0

	err := provisioner.RetryTransientTalosAPICallForTest(
		t.Context(), "1.2.3.4", "Config apply",
		func(_ context.Context) error {
			calls++
			if calls < testRetryMaxAttempts {
				return fmt.Errorf("apply: %w", transientErr)
			}

			return nil
		})

	require.NoError(t, err)
	assert.Equal(t, testRetryMaxAttempts, calls)
	assert.Contains(t, logBuf.String(), "Config apply attempt 1/3 failed on 1.2.3.4")
	assert.Contains(t, logBuf.String(), "Config apply attempt 2/3 failed on 1.2.3.4")
}

func TestRetryTransientTalosAPICall_RetriesDeadlineExceeded(t *testing.T) {
	t.Parallel()

	provisioner, _ := newRetryTestProvisioner()
	calls := 0

	err := provisioner.RetryTransientTalosAPICallForTest(
		t.Context(), "1.2.3.4", "Machine config fetch",
		func(_ context.Context) error {
			calls++
			if calls == 1 {
				return status.Error(codes.DeadlineExceeded, "context deadline exceeded")
			}

			return nil
		})

	require.NoError(t, err)
	assert.Equal(t, 2, calls)
}

func TestRetryTransientTalosAPICall_NonTransientFailsImmediately(t *testing.T) {
	t.Parallel()

	provisioner, logBuf := newRetryTestProvisioner()
	calls := 0

	err := provisioner.RetryTransientTalosAPICallForTest(
		t.Context(), "1.2.3.4", "Config apply",
		func(_ context.Context) error {
			calls++

			return errPermissionDenied
		})

	require.ErrorIs(t, err, errPermissionDenied)
	require.NotErrorIs(t, err, talosprovisioner.ErrRetriesExhaustedForTest)
	assert.Equal(t, 1, calls)
	assert.Empty(t, logBuf.String())
}

func TestRetryTransientTalosAPICall_ExhaustsAttempts(t *testing.T) {
	t.Parallel()

	provisioner, logBuf := newRetryTestProvisioner()
	transientErr := status.Error(codes.Unavailable, "authentication handshake failed")
	calls := 0

	err := provisioner.RetryTransientTalosAPICallForTest(
		t.Context(), "1.2.3.4", "Config apply",
		func(_ context.Context) error {
			calls++

			return fmt.Errorf("apply: %w", transientErr)
		})

	require.ErrorIs(t, err, talosprovisioner.ErrRetriesExhaustedForTest)
	require.ErrorIs(t, err, transientErr)
	assert.Equal(t, testRetryMaxAttempts, calls)
	assert.Contains(t, logBuf.String(), "Config apply attempt 2/3 failed on 1.2.3.4")
}

func TestRetryTransientTalosAPICall_StopsWhenContextCancelled(t *testing.T) {
	t.Parallel()

	provisioner, _ := newRetryTestProvisioner()
	transientErr := status.Error(codes.Unavailable, "connection error")
	calls := 0

	ctx, cancel := context.WithCancel(t.Context())

	err := provisioner.RetryTransientTalosAPICallForTest(
		ctx, "1.2.3.4", "Config apply",
		func(_ context.Context) error {
			calls++

			cancel()

			return fmt.Errorf("apply: %w", transientErr)
		})

	require.ErrorIs(t, err, transientErr)
	require.NotErrorIs(t, err, talosprovisioner.ErrRetriesExhaustedForTest)
	assert.Equal(t, 1, calls)
}

func TestRetryTransientTalosAPICall_BackoffInterrupted(t *testing.T) {
	t.Parallel()

	logBuf := &bytes.Buffer{}
	// Long backoff so the context deadline expires mid-sleep.
	provisioner := talosprovisioner.NewProvisioner(nil, nil).
		WithLogWriter(logBuf).
		WithTalosAPIRetryConfig(testRetryMaxAttempts, time.Minute, time.Minute)

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()

	calls := 0

	err := provisioner.RetryTransientTalosAPICallForTest(
		ctx, "1.2.3.4", "Config apply",
		func(_ context.Context) error {
			calls++

			return status.Error(codes.Unavailable, "connection error")
		})

	require.ErrorContains(t, err, "retry backoff interrupted")
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Equal(t, 1, calls)
}
