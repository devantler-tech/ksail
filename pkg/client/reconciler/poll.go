package reconciler

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// permanentFailure wraps a non-transient, non-context check error in the
// "permanent failure" message the GitOps poll loops have always used.
func permanentFailure(err error) error {
	return fmt.Errorf("permanent failure: %w", err)
}

// CheckResult is the outcome of one readiness probe performed by a
// [PollUntilReady] check function.
type CheckResult struct {
	// Ready reports whether the resource has reached its desired state.
	Ready bool
	// Status is an optional human-readable status carried into the timeout
	// message (e.g. a Flux Kustomization's last-applied status). Empty when the
	// engine exposes no per-poll status.
	Status string
}

// haltError wraps an error that [PollUntilReady] must return verbatim, bypassing
// the default "permanent failure" wrapping. It lets a check function finalize an
// outcome (e.g. a fail-fast dependency error) on its own terms.
type haltError struct {
	err error
}

func (e *haltError) Error() string { return e.err.Error() }
func (e *haltError) Unwrap() error { return e.err }

// Halt marks err so that [PollUntilReady] returns it unchanged instead of
// wrapping it as a permanent failure. Use it for check-side fail-fast paths that
// already produced their final error.
func Halt(err error) error {
	if err == nil {
		return nil
	}

	return &haltError{err: err}
}

// PollUntilReady polls check every interval until it reports Ready, the context
// is cancelled, or its deadline expires.
//
// The error contract preserves the hand-rolled GitOps poll loops it replaces:
//
//   - check ready → nil;
//   - check returns a context error (per [IsContextError]) → context.Canceled
//     propagates verbatim, otherwise timeoutErr(lastStatus) is returned;
//   - check returns an error wrapped with [Halt] → that error verbatim;
//   - check returns any other error → wrapped "permanent failure: %w";
//   - the inter-poll wait is cancelled → context.Canceled propagates verbatim,
//     otherwise timeoutErr(lastStatus).
//
// timeoutErr receives the most recent non-empty Status so callers can embed it
// in their actionable timeout message.
func PollUntilReady(
	ctx context.Context,
	interval time.Duration,
	check func(ctx context.Context) (CheckResult, error),
	timeoutErr func(lastStatus string) error,
) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var lastStatus string

	for {
		result, err := check(ctx)
		if err != nil {
			return classifyCheckError(ctx, err, lastStatus, timeoutErr)
		}

		if result.Ready {
			return nil
		}

		lastStatus = result.Status

		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.Canceled) {
				return ctx.Err() //nolint:wrapcheck // propagate cancellation as-is
			}

			return timeoutErr(lastStatus)
		case <-ticker.C:
		}
	}
}

// classifyCheckError maps a non-nil check error to the final poll error,
// matching the legacy loops' branching.
func classifyCheckError(
	ctx context.Context,
	err error,
	lastStatus string,
	timeoutErr func(lastStatus string) error,
) error {
	var halt *haltError
	if errors.As(err, &halt) {
		return halt.err
	}

	if IsContextError(err) {
		if errors.Is(ctx.Err(), context.Canceled) {
			return ctx.Err() //nolint:wrapcheck // propagate cancellation as-is
		}

		return timeoutErr(lastStatus)
	}

	return permanentFailure(err)
}
