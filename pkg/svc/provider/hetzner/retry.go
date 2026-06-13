package hetzner

import (
	"context"
	"fmt"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/client/netretry"
)

// calculateRetryDelay returns the exponential-backoff delay for the given
// 1-based attempt, capped at [DefaultRetryMaxDelay]. It delegates to
// [netretry.ExponentialDelay], the single source of truth for the
// min(base*2^(attempt-1), max) formula.
func (p *Provider) calculateRetryDelay(attempt int) time.Duration {
	return netretry.ExponentialDelay(attempt, DefaultRetryBaseDelay, DefaultRetryMaxDelay)
}

// waitForBackoff blocks for delay, returning a non-nil error wrapped with cause
// (e.g. "context cancelled during retry") if the context is cancelled first.
// This is the single ctx-aware wait shared by every hetzner retry loop, so a
// Ctrl-C during a delete or scale-up no longer hangs through an uncancellable
// sleep.
func waitForBackoff(ctx context.Context, cause string, delay time.Duration) error {
	select {
	case <-ctx.Done():
		return fmt.Errorf("%s: %w", cause, ctx.Err())
	case <-time.After(delay):
		return nil
	}
}

// retryDelete runs a fixed-delay, ctx-aware delete loop up to maxAttempts times.
// deleteOp performs one delete attempt and returns (done, err): done=true stops
// the loop immediately (success or idempotent not-found), a non-nil err is the
// failure to report once attempts are exhausted. terminal wraps the last error
// into the caller's exact message. The wait between attempts is cancellable.
func retryDelete(
	ctx context.Context,
	maxAttempts int,
	delay time.Duration,
	cause string,
	deleteOp func() (done bool, err error),
	terminal func(lastErr error) error,
) error {
	var lastErr error

	for attempt := range maxAttempts {
		done, err := deleteOp()
		if done {
			return nil
		}

		lastErr = err

		if attempt == maxAttempts-1 {
			return terminal(lastErr)
		}

		waitErr := waitForBackoff(ctx, cause, delay)
		if waitErr != nil {
			return waitErr
		}
	}

	return nil
}
