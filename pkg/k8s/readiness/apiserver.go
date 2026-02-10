package readiness

import (
	"context"
	"time"

	"k8s.io/client-go/kubernetes"
)

// WaitForAPIServerReady waits for the Kubernetes API server to be ready and stable.
//
// This function polls the API server by performing a ServerVersion request until it
// responds consistently without errors. This is useful after cluster bootstrap when
// the API server may be unstable due to initial startup.
//
// The function uses the configured polling interval and will timeout after the
// specified deadline.
//
// Parameters:
//   - ctx: Context for cancellation
//   - clientset: Kubernetes client interface
//   - deadline: Maximum time to wait for API server readiness
//
// Returns an error if the API server is not ready within the deadline.
func WaitForAPIServerReady(
	ctx context.Context,
	clientset kubernetes.Interface,
	deadline time.Duration,
) error {
	return PollForReadiness(ctx, deadline, func(_ context.Context) (bool, error) {
		// Use ServerVersion as a lightweight health check
		_, err := clientset.Discovery().ServerVersion()
		if err != nil {
			// Continue polling on any error - the API server is not ready yet
			return false, nil //nolint:nilerr // returning nil to continue polling
		}

		return true, nil
	})
}

// WaitForAPIServerStable waits for the Kubernetes API server to respond consistently.
//
// Unlike WaitForAPIServerReady which returns on the first successful response,
// this function requires multiple consecutive successful responses to ensure
// the API server is truly stable. This is particularly useful for Talos
// where the API server may respond once but then fail with connection resets.
//
// Parameters:
//   - ctx: Context for cancellation
//   - clientset: Kubernetes client interface
//   - deadline: Maximum time to wait for API server stability
//   - requiredSuccesses: Number of consecutive successful responses required
//
// Returns an error if the API server is not stable within the deadline.
func WaitForAPIServerStable(
	ctx context.Context,
	clientset kubernetes.Interface,
	deadline time.Duration,
	requiredSuccesses int,
) error {
	if requiredSuccesses < 1 {
		requiredSuccesses = 1
	}

	consecutiveSuccesses := 0

	return PollForReadiness(ctx, deadline, func(_ context.Context) (bool, error) {
		_, err := clientset.Discovery().ServerVersion()
		if err != nil {
			// Reset counter on any error
			consecutiveSuccesses = 0

			return false, nil //nolint:nilerr // returning nil to continue polling
		}

		consecutiveSuccesses++

		if consecutiveSuccesses >= requiredSuccesses {
			return true, nil
		}

		return false, nil
	})
}
