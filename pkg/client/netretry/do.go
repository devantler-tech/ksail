package netretry

import (
	"context"
	"errors"
	"time"
)

// cancelledError wraps the error returned when [Do] stops because its backoff
// wait observed a cancelled context. It lets callers distinguish a genuine
// cancellation from an operation error that merely happens to wrap a context
// error, via [IsCancelled], regardless of the message [WithCancelError] adds.
type cancelledError struct {
	err error
}

func (e *cancelledError) Error() string { return e.err.Error() }
func (e *cancelledError) Unwrap() error { return e.err }

// IsCancelled reports whether err originated from [Do] aborting on a cancelled
// context during its backoff wait (as opposed to an operation error). Use it to
// decide whether to apply per-operation failure wrapping.
func IsCancelled(err error) bool {
	var ce *cancelledError

	return errors.As(err, &ce)
}

// doConfig holds the resolved options for a [Do] call.
type doConfig struct {
	isRetryable func(error) bool
	cancelError func(error) error
	onRetry     func(attempt int, delay time.Duration, err error)
}

// Option customizes the behavior of [Do].
type Option func(*doConfig)

// WithRetryable overrides the predicate used to decide whether an error is
// transient and should be retried. By default [Do] uses [IsRetryable]. Callers
// with domain-specific transient errors (e.g. Copilot's "fetch failed") pass a
// wider predicate here.
func WithRetryable(pred func(error) bool) Option {
	return func(cfg *doConfig) {
		if pred != nil {
			cfg.isRetryable = pred
		}
	}
}

// WithCancelError maps the context error observed when the backoff wait is
// cancelled to the error [Do] returns. This lets each caller keep its exact
// cancellation wrapping (e.g. "push cancelled: %w"). When unset, [Do] returns
// the raw context error.
func WithCancelError(wrap func(ctxErr error) error) Option {
	return func(cfg *doConfig) {
		if wrap != nil {
			cfg.cancelError = wrap
		}
	}
}

// WithOnRetry registers a side-effect callback invoked once before each backoff
// wait, after a retryable failure. It receives the just-completed attempt
// number, the upcoming delay, and the error being retried. Used by callers that
// emit per-retry notifications.
func WithOnRetry(hook func(attempt int, delay time.Duration, err error)) Option {
	return func(cfg *doConfig) {
		if hook != nil {
			cfg.onRetry = hook
		}
	}
}

// Do runs operation up to attempts times, retrying while the error is transient
// according to the configured predicate (default [IsRetryable]) and applying
// exponential backoff (see [ExponentialDelay]) between attempts.
//
// Do returns nil on the first success. When all attempts are exhausted, or the
// error is non-retryable, it returns the LAST error from operation unwrapped so
// callers can apply their own wrapping (preserving sentinel identity via
// errors.Is). If the context is cancelled during a backoff wait, Do returns the
// context error mapped through the [WithCancelError] option (or the raw context
// error when that option is unset).
//
// attempts below 1 is treated as 1 (a single attempt with no retries).
func Do(
	ctx context.Context,
	attempts int,
	baseWait, maxWait time.Duration,
	operation func() error,
	opts ...Option,
) error {
	cfg := doConfig{isRetryable: IsRetryable}
	for _, opt := range opts {
		opt(&cfg)
	}

	if attempts < 1 {
		attempts = 1
	}

	var lastErr error

	for attempt := 1; attempt <= attempts; attempt++ {
		lastErr = operation()
		if lastErr == nil {
			return nil
		}

		if !cfg.isRetryable(lastErr) || attempt == attempts {
			break
		}

		waitErr := backoffWait(ctx, &cfg, attempt, baseWait, maxWait, lastErr)
		if waitErr != nil {
			return waitErr
		}
	}

	return lastErr
}

// backoffWait sleeps for the exponential-backoff delay for the given attempt,
// returning a non-nil error if the context is cancelled so [Do] stops retrying.
func backoffWait(
	ctx context.Context,
	cfg *doConfig,
	attempt int,
	baseWait, maxWait time.Duration,
	lastErr error,
) error {
	// Stop immediately if the context is already cancelled. Without this check
	// the backoff timer races ctx.Done() in the select below: when the goroutine
	// is descheduled past a sub-millisecond delay both cases are ready and select
	// picks one at random, sometimes running an extra attempt.
	ctxErr := ctx.Err()
	if ctxErr != nil {
		return mapCancel(cfg, ctxErr)
	}

	delay := ExponentialDelay(attempt, baseWait, maxWait)

	if cfg.onRetry != nil {
		cfg.onRetry(attempt, delay, lastErr)
	}

	timer := time.NewTimer(delay)

	select {
	case <-ctx.Done():
		if !timer.Stop() {
			<-timer.C
		}

		return mapCancel(cfg, ctx.Err())
	case <-timer.C:
		// Prioritize cancellation even when the timer also fired: select picks a
		// ready case at random, so a context cancelled during the wait can still
		// land here. Re-checking keeps cancellation deterministic and prevents one
		// extra retry attempt.
		timerCtxErr := ctx.Err()
		if timerCtxErr != nil {
			return mapCancel(cfg, timerCtxErr)
		}

		return nil
	}
}

// mapCancel applies the configured cancel-error mapping (or returns the raw
// context error when no mapping is set) and tags the result as a cancellation so
// [IsCancelled] can recognize it.
func mapCancel(cfg *doConfig, ctxErr error) error {
	mapped := ctxErr
	if cfg.cancelError != nil {
		mapped = cfg.cancelError(ctxErr)
	}

	return &cancelledError{err: mapped}
}
