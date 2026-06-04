package kubeconform_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/client/kubeconform"
	"github.com/stretchr/testify/require"
)

// Static sentinel errors for the retry tests (err113: avoid inline dynamic errors).
var (
	// errTransientEOF mimics a truncated schema download (netretry-retryable).
	errTransientEOF = errors.New("unexpected EOF")
	// errTransientReset mimics a dropped connection (netretry-retryable).
	errTransientReset = errors.New("connection reset by peer")
	// errNonTransient mimics a real schema-validation failure (not retryable).
	errNonTransient = errors.New("schema invalid: required field missing")
)

// newFastRetryClient returns a client with negligible backoff so retry tests
// stay fast while keeping the default attempt count. Each test owns its own
// client, so mutating its fields is parallel-safe (no shared global state).
func newFastRetryClient() *kubeconform.Client {
	client := kubeconform.NewClient()
	client.SetRetryConfig(client.MaxRetryAttempts(), time.Millisecond, time.Millisecond)

	return client
}

func TestValidateWithRetrySucceedsAfterTransientError(t *testing.T) {
	t.Parallel()

	client := newFastRetryClient()
	calls := 0

	err := kubeconform.ValidateWithRetry(client, context.Background(), func() error {
		calls++
		if calls < 2 {
			return errTransientEOF
		}

		return nil
	})
	require.NoError(t, err, "expected success after a transient retry")
	require.Equal(t, 2, calls, "expected 2 attempts")
}

func TestValidateWithRetryDoesNotRetryNonTransientError(t *testing.T) {
	t.Parallel()

	client := newFastRetryClient()
	calls := 0

	err := kubeconform.ValidateWithRetry(client, context.Background(), func() error {
		calls++

		return errNonTransient
	})
	require.ErrorIs(t, err, errNonTransient, "expected the original error")
	require.Equal(t, 1, calls, "expected 1 attempt for a non-transient error")
}

func TestValidateWithRetryGivesUpAfterMaxAttempts(t *testing.T) {
	t.Parallel()

	client := newFastRetryClient()
	calls := 0

	err := kubeconform.ValidateWithRetry(client, context.Background(), func() error {
		calls++

		return errTransientReset
	})
	require.Error(t, err, "expected an error after exhausting all attempts")
	require.Equal(t, client.MaxRetryAttempts(), calls, "expected to exhaust every attempt")
}

func TestValidateWithRetryStopsOnContextCancel(t *testing.T) {
	t.Parallel()

	// Use the default (non-trivial) backoff so the cancel is observed during the wait.
	client := kubeconform.NewClient()
	calls := 0
	ctx, cancel := context.WithCancel(context.Background())

	err := kubeconform.ValidateWithRetry(client, ctx, func() error {
		calls++

		cancel()

		return errTransientEOF
	})
	require.ErrorIs(t, err, context.Canceled, "expected context.Canceled")
	require.Equal(t, 1, calls, "expected 1 attempt before cancellation")
}

func TestValidateWithRetryTreatsZeroAttemptsAsOne(t *testing.T) {
	t.Parallel()

	client := kubeconform.NewClient()
	client.SetRetryConfig(0, time.Millisecond, time.Millisecond)

	calls := 0

	err := kubeconform.ValidateWithRetry(client, context.Background(), func() error {
		calls++

		return errTransientEOF
	})
	require.Error(t, err, "expected an error")
	require.Equal(t, 1, calls, "expected exactly 1 attempt")
}
