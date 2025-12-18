package k8s

import (
	"context"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
)

const (
	// Polling configuration.

	// readinessPollInterval is the interval between readiness checks.
	readinessPollInterval = 2 * time.Second
)

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
// Returns an error if polling times out or if the poll function returns an error.
func PollForReadiness(
	ctx context.Context,
	deadline time.Duration,
	poll func(context.Context) (bool, error),
) error {
	pollErr := wait.PollUntilContextTimeout(
		ctx,
		readinessPollInterval,
		deadline,
		true,
		poll,
	)
	if pollErr != nil {
		return fmt.Errorf("failed to poll for readiness: %w", pollErr)
	}

	return nil
}
