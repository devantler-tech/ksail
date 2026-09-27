// Package retry provides a shared create-with-retry loop for cluster
// provisioners whose underlying tooling has no built-in transient-failure
// recovery (currently VCluster and KWOK). It factors out the attempt loop,
// between-attempt cleanup, context-aware backoff, transient-error classification,
// and terminal-error wrapping that both provisioners previously hand-rolled,
// while preserving KWOK's per-attempt timeout through a hook.
package retry

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Config parameterizes a [Do] run. The zero value is not usable; at minimum
// MaxAttempts, Attempt, and IsTransient must be set.
type Config struct {
	// MaxAttempts is the total number of create attempts (>= 1).
	MaxAttempts int
	// RetryDelay is the backoff slept before each retry attempt (after the first).
	RetryDelay time.Duration
	// AttemptTimeout, when > 0, bounds each individual attempt so a stalled
	// create is interrupted and retried instead of hanging. A timeout that fires
	// while the parent context is still alive is treated as transient.
	AttemptTimeout time.Duration

	// Attempt performs one create attempt. It receives an attempt-scoped context
	// (derived from the parent and bounded by AttemptTimeout when set).
	Attempt func(ctx context.Context) error
	// Cleanup, when non-nil, is called before each retry attempt to remove
	// partially-created state. It is never called before the first attempt.
	Cleanup func(ctx context.Context)
	// IsTransient reports whether a create error may succeed on retry.
	IsTransient func(error) bool

	// Logf logs progress (retry notices, transient/timeout diagnostics). When nil
	// no logging is performed. attempt is 1-based.
	Logf func(format string, args ...any)

	// WrapNonTransient wraps the error returned when a non-transient failure
	// aborts the loop. When nil the raw error is returned.
	WrapNonTransient func(err error) error
	// WrapExhausted wraps the error returned when all attempts are exhausted. It
	// receives the attempt count and the last error.
	WrapExhausted func(attempts int, err error) error
}

// Do runs the create-with-retry loop described by cfg and returns nil on the
// first success, the (optionally wrapped) last error otherwise.
func Do(ctx context.Context, cfg Config) error {
	var lastErr error

	for attempt := range cfg.MaxAttempts {
		if attempt > 0 {
			waitErr := cfg.prepareRetry(ctx, attempt)
			if waitErr != nil {
				return waitErr
			}
		}

		var timedOut bool

		timedOut, lastErr = cfg.runAttempt(ctx, attempt)
		if lastErr == nil {
			return nil
		}

		// A per-attempt timeout is treated as transient and retried directly;
		// runAttempt already logged it, so skip further classification/logging.
		if timedOut {
			continue
		}

		if !cfg.IsTransient(lastErr) {
			return cfg.wrapNonTransient(lastErr)
		}

		cfg.logf(
			"create attempt %d/%d failed (transient): %v",
			attempt+1, cfg.MaxAttempts, lastErr,
		)
	}

	return cfg.wrapExhausted(lastErr)
}

// prepareRetry logs the retry, runs cleanup, and waits the backoff delay,
// returning a non-nil error if the context is cancelled.
func (cfg Config) prepareRetry(ctx context.Context, attempt int) error {
	cfg.logf("Retrying cluster create (attempt %d/%d)...", attempt+1, cfg.MaxAttempts)

	if cfg.Cleanup != nil {
		cfg.Cleanup(ctx)
	}

	select {
	case <-ctx.Done():
		return fmt.Errorf("context cancelled during create retry: %w", ctx.Err())
	case <-time.After(cfg.RetryDelay):
		return nil
	}
}

// runAttempt runs one create attempt, applying AttemptTimeout when set. It
// reports whether the attempt was aborted by its own timeout (transient) and the
// create error.
func (cfg Config) runAttempt(ctx context.Context, attempt int) (bool, error) {
	if cfg.AttemptTimeout <= 0 {
		return false, cfg.Attempt(ctx)
	}

	attemptCtx, cancel := context.WithTimeout(ctx, cfg.AttemptTimeout)
	defer cancel()

	err := cfg.Attempt(attemptCtx)
	if err == nil {
		return false, nil
	}

	// A timeout is transient only when the per-attempt deadline fired while the
	// parent context was still alive; a cancelled parent must propagate.
	timedOut := errors.Is(attemptCtx.Err(), context.DeadlineExceeded) && ctx.Err() == nil
	if timedOut {
		cfg.logf(
			"create attempt %d/%d timed out after %s (treating as transient): %v",
			attempt+1, cfg.MaxAttempts, cfg.AttemptTimeout, err,
		)
	}

	return timedOut, err
}

func (cfg Config) logf(format string, args ...any) {
	if cfg.Logf != nil {
		cfg.Logf(format, args...)
	}
}

func (cfg Config) wrapNonTransient(err error) error {
	if cfg.WrapNonTransient != nil {
		return cfg.WrapNonTransient(err)
	}

	return err
}

func (cfg Config) wrapExhausted(err error) error {
	if cfg.WrapExhausted != nil {
		return cfg.WrapExhausted(cfg.MaxAttempts, err)
	}

	return err
}
