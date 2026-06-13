package netretry_test

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/client/netretry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	errTransient    = errors.New("connection reset by peer")
	errPermanent    = errors.New("permanent failure")
	errSentinelTest = errors.New("sentinel")
)

func TestDoSucceedsFirstAttempt(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32

	err := netretry.Do(t.Context(), 3, time.Millisecond, time.Millisecond, func() error {
		calls.Add(1)

		return nil
	})

	require.NoError(t, err)
	assert.Equal(t, int32(1), calls.Load())
}

func TestDoRetriesTransientThenSucceeds(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32

	err := netretry.Do(t.Context(), 5, time.Microsecond, time.Microsecond, func() error {
		if calls.Add(1) < 3 {
			return errTransient
		}

		return nil
	})

	require.NoError(t, err)
	assert.Equal(t, int32(3), calls.Load())
}

func TestDoReturnsLastErrorOnExhaustion(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32

	err := netretry.Do(t.Context(), 3, time.Microsecond, time.Microsecond, func() error {
		calls.Add(1)

		return errTransient
	})

	require.ErrorIs(t, err, errTransient)
	assert.Equal(t, int32(3), calls.Load())
	assert.False(t, netretry.IsCancelled(err))
}

func TestDoStopsOnNonRetryable(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32

	err := netretry.Do(t.Context(), 5, time.Microsecond, time.Microsecond, func() error {
		calls.Add(1)

		return errPermanent
	})

	require.ErrorIs(t, err, errPermanent)
	assert.Equal(t, int32(1), calls.Load(), "non-retryable error must not retry")
}

func TestDoPreservesSentinelIdentity(t *testing.T) {
	t.Parallel()

	wrapped := fmt.Errorf("validation: %w", errSentinelTest)

	err := netretry.Do(t.Context(), 2, time.Microsecond, time.Microsecond, func() error {
		return wrapped
	})

	require.ErrorIs(t, err, errSentinelTest)
}

func TestDoCancelDuringBackoffReturnsTaggedCancel(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())

	var calls atomic.Int32

	go func() {
		// Cancel after the first failure schedules a backoff wait.
		for calls.Load() < 1 {
			time.Sleep(time.Millisecond)
		}

		cancel()
	}()

	err := netretry.Do(ctx, 5, 50*time.Millisecond, time.Second, func() error {
		calls.Add(1)

		return errTransient
	})

	require.Error(t, err)
	assert.True(t, netretry.IsCancelled(err), "cancellation during backoff must be tagged")
	require.ErrorIs(t, err, context.Canceled)
}

func TestDoCancelErrorMappingPreservesText(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := netretry.Do(
		ctx,
		3,
		time.Millisecond,
		time.Millisecond,
		func() error { return errTransient },
		netretry.WithCancelError(func(ctxErr error) error {
			return fmt.Errorf("custom cancel: %w", ctxErr)
		}),
	)

	require.Error(t, err)
	assert.True(t, netretry.IsCancelled(err))
	assert.Contains(t, err.Error(), "custom cancel:")
	require.ErrorIs(t, err, context.Canceled)
}

func TestDoRetryablePredicateOverride(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32

	// errPermanent is not transient per IsRetryable; the override makes it so.
	err := netretry.Do(
		t.Context(),
		3,
		time.Microsecond,
		time.Microsecond,
		func() error {
			calls.Add(1)

			return errPermanent
		},
		netretry.WithRetryable(func(error) bool { return true }),
	)

	require.ErrorIs(t, err, errPermanent)
	assert.Equal(t, int32(3), calls.Load(), "override must enable retries")
}

func TestDoOnRetryHookInvokedPerRetry(t *testing.T) {
	t.Parallel()

	var (
		calls   atomic.Int32
		retries atomic.Int32
	)

	_ = netretry.Do(
		t.Context(),
		3,
		time.Microsecond,
		time.Microsecond,
		func() error {
			calls.Add(1)

			return errTransient
		},
		netretry.WithOnRetry(func(_ int, _ time.Duration, _ error) {
			retries.Add(1)
		}),
	)

	// 3 attempts → 2 backoff waits → hook fires twice.
	assert.Equal(t, int32(3), calls.Load())
	assert.Equal(t, int32(2), retries.Load())
}

func TestDoZeroAttemptsRunsOnce(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32

	err := netretry.Do(t.Context(), 0, time.Microsecond, time.Microsecond, func() error {
		calls.Add(1)

		return errTransient
	})

	require.ErrorIs(t, err, errTransient)
	assert.Equal(t, int32(1), calls.Load())
}
