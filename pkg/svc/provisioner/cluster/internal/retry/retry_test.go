package retry_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/internal/retry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	errTransientRetry = errors.New("transient boom")
	errPermanentRetry = errors.New("permanent boom")
	errSpecialRetry   = errors.New("special boom")
	errRecoveryRetry  = errors.New("recovery failed")
)

func isTransient(err error) bool {
	return errors.Is(err, errTransientRetry)
}

func TestDoSucceedsFirstAttempt(t *testing.T) {
	t.Parallel()

	var attempts int

	err := retry.Do(t.Context(), retry.Config{
		MaxAttempts: 3,
		RetryDelay:  time.Microsecond,
		Attempt: func(context.Context) error {
			attempts++

			return nil
		},
		IsTransient: isTransient,
	})

	require.NoError(t, err)
	assert.Equal(t, 1, attempts)
}

func TestDoRetriesTransientThenExhausts(t *testing.T) {
	t.Parallel()

	var (
		attempts int
		cleanups int
	)

	err := retry.Do(t.Context(), retry.Config{
		MaxAttempts: 3,
		RetryDelay:  time.Microsecond,
		Attempt: func(context.Context) error {
			attempts++

			return errTransientRetry
		},
		Cleanup:     func(context.Context) { cleanups++ },
		IsTransient: isTransient,
		WrapExhausted: func(n int, err error) error {
			return fmt.Errorf("failed after %d attempts: %w", n, err)
		},
	})

	require.ErrorIs(t, err, errTransientRetry)
	assert.Contains(t, err.Error(), "failed after 3 attempts")
	assert.Equal(t, 3, attempts)
	assert.Equal(t, 2, cleanups, "cleanup runs before each retry")
}

func TestDoNonTransientFailsImmediately(t *testing.T) {
	t.Parallel()

	var attempts int

	err := retry.Do(t.Context(), retry.Config{
		MaxAttempts: 5,
		RetryDelay:  time.Microsecond,
		Attempt: func(context.Context) error {
			attempts++

			return errPermanentRetry
		},
		IsTransient: isTransient,
		WrapNonTransient: func(err error) error {
			return fmt.Errorf("create failed: %w", err)
		},
	})

	require.ErrorIs(t, err, errPermanentRetry)
	assert.Contains(t, err.Error(), "create failed")
	assert.Equal(t, 1, attempts)
}

func TestDoOnSpecialErrorRecovered(t *testing.T) {
	t.Parallel()

	var attempts int

	err := retry.Do(t.Context(), retry.Config{
		MaxAttempts: 3,
		RetryDelay:  time.Microsecond,
		Attempt: func(context.Context) error {
			attempts++

			return errSpecialRetry
		},
		IsTransient: isTransient,
		OnSpecialError: func(_ context.Context, _ int, err error) (retry.SpecialResult, error) {
			if errors.Is(err, errSpecialRetry) {
				return retry.Recovered, nil
			}

			return retry.NotSpecial, nil
		},
	})

	require.NoError(t, err)
	assert.Equal(t, 1, attempts, "recovered special error stops the loop")
}

func TestDoOnSpecialErrorRetryFreshThenExhaust(t *testing.T) {
	t.Parallel()

	var attempts int

	err := retry.Do(t.Context(), retry.Config{
		MaxAttempts: 2,
		RetryDelay:  time.Microsecond,
		Attempt: func(context.Context) error {
			attempts++

			return errSpecialRetry
		},
		IsTransient: isTransient,
		OnSpecialError: func(_ context.Context, _ int, _ error) (retry.SpecialResult, error) {
			return retry.RetryFresh, errRecoveryRetry
		},
		WrapExhausted: func(n int, err error) error {
			return fmt.Errorf("failed after %d attempts: %w", n, err)
		},
	})

	require.ErrorIs(t, err, errRecoveryRetry)
	assert.Contains(t, err.Error(), "failed after 2 attempts")
	assert.Equal(t, 2, attempts)
}

func TestDoAttemptTimeoutTreatedAsTransient(t *testing.T) {
	t.Parallel()

	var attempts int

	err := retry.Do(t.Context(), retry.Config{
		MaxAttempts:    2,
		RetryDelay:     time.Microsecond,
		AttemptTimeout: time.Millisecond,
		Attempt: func(ctx context.Context) error {
			attempts++

			<-ctx.Done() // exceed the per-attempt timeout

			return ctx.Err()
		},
		IsTransient: isTransient, // ctx.Err() is NOT transient per this predicate
		WrapExhausted: func(n int, err error) error {
			return fmt.Errorf("failed after %d attempts: %w", n, err)
		},
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed after 2 attempts")
	assert.Equal(t, 2, attempts, "timeout is retried despite non-transient predicate")
}

func TestDoParentCancelStopsLoop(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())

	var attempts int

	err := retry.Do(ctx, retry.Config{
		MaxAttempts: 5,
		RetryDelay:  time.Hour, // long delay so cancel wins the select
		Attempt: func(context.Context) error {
			attempts++

			cancel()

			return errTransientRetry
		},
		IsTransient: isTransient,
	})

	require.ErrorContains(t, err, "context cancelled during create retry")
	assert.Equal(t, 1, attempts)
}
