package readiness

import (
	"context"
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/wait"
)

const (
	// Polling configuration.

	// readinessPollInterval is the interval between readiness checks.
	readinessPollInterval = 2 * time.Second
)

// pollIfExists shares one deadline between the initial lookup and readiness polling.
// The lookup supplies resource-specific error context; a missing resource is skipped.
func pollIfExists(
	ctx context.Context,
	deadline time.Duration,
	lookup func(context.Context) error,
	poll func(context.Context) (bool, error),
) error {
	deadlineCtx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()

	err := lookup(deadlineCtx)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}

		return err
	}

	return PollForReadiness(deadlineCtx, 0, poll)
}

// PollForReadiness polls a check function until ready or timeout.
//
// This function repeatedly calls the provided poll function at regular intervals
// until either:
//   - The poll function returns (true, nil) indicating readiness
//   - The deadline is exceeded
//   - The poll function returns an error
//
// The poll function should return (false, nil) to continue polling,
// (true, nil) when the resource is ready, or (false, error) on errors.
//
// When deadline is 0, polling is bounded solely by ctx (e.g. a context
// created with context.WithTimeout by the caller). When deadline is
// non-zero, polling uses deadline as a timeout duration, unless ctx is
// canceled or reaches its own deadline sooner.
//
// Returns an error if polling times out or if the poll function returns an error.
func PollForReadiness(
	ctx context.Context,
	deadline time.Duration,
	poll func(context.Context) (bool, error),
) error {
	var pollErr error
	if deadline == 0 {
		pollErr = wait.PollUntilContextCancel(ctx, readinessPollInterval, true, poll)
	} else {
		pollErr = wait.PollUntilContextTimeout(ctx, readinessPollInterval, deadline, true, poll)
	}

	if pollErr != nil {
		return fmt.Errorf("failed to poll for readiness: %w", pollErr)
	}

	return nil
}
