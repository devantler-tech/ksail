package flux

import (
	"context"
	"fmt"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
)

// API discovery substrings used to detect that the Flux CRDs or controllers
// are not yet ready.
const (
	apiDiscoveryNotFoundSubstr      = "the server could not find the requested resource"
	apiDiscoveryNoMatchesKindSubstr = "no matches for kind"
)

// API availability timeout for reconciliation operations - should be long enough
// for the Flux controllers to become ready in slow CI environments.
const (
	apiAvailabilityTimeout      = 2 * time.Minute
	apiAvailabilityPollInterval = 500 * time.Millisecond

	reconcileAnnotationKey = "reconcile.fluxcd.io/requestedAt"
)

// isAPIDiscoveryError checks if the error indicates the API discovery is incomplete.
func isAPIDiscoveryError(errMsg string) bool {
	// "the server could not find the requested resource" indicates the CRD endpoint
	// isn't fully registered yet or the Flux controllers haven't started
	if strings.Contains(errMsg, apiDiscoveryNotFoundSubstr) {
		return true
	}

	// "no matches for kind" is a REST mapper error when the CRD isn't known yet
	return strings.Contains(errMsg, apiDiscoveryNoMatchesKindSubstr)
}

// isConnectionError checks if the error is a network connection error.
//
// The substrings deliberately differ from netretry's transient patterns (e.g.
// "EOF" here vs "unexpected EOF" there, "connection reset" vs "connection reset
// by peer"); delegating to netretry would widen matching, so this check is kept
// independent on purpose.
func isConnectionError(errMsg string) bool {
	return strings.Contains(errMsg, "connection refused") ||
		strings.Contains(errMsg, "connection reset") ||
		strings.Contains(errMsg, "i/o timeout") ||
		strings.Contains(errMsg, "EOF")
}

// isTransientAPIError checks if the error is a transient API error that should be retried.
// This includes errors that occur when the Flux CRDs or controllers aren't fully ready yet,
// which can happen in slow CI environments or shortly after cluster creation.
func isTransientAPIError(err error) bool {
	if err == nil {
		return false
	}

	// Check for specific status errors that indicate the API isn't ready
	if apierrors.IsServiceUnavailable(err) ||
		apierrors.IsTimeout(err) ||
		apierrors.IsTooManyRequests(err) ||
		apierrors.IsConflict(err) {
		return true
	}

	// NotFound is transient because the resource may not exist yet.
	// The Instance controller creates OCIRepository and Kustomization resources
	// asynchronously, so they might not exist immediately after Instance creation.
	// The retry loop has a timeout, so if the resource truly doesn't exist, it will fail.
	if apierrors.IsNotFound(err) {
		return true
	}

	errMsg := err.Error()

	// Check for API discovery errors
	if isAPIDiscoveryError(errMsg) {
		return true
	}

	// Check for connection errors
	return isConnectionError(errMsg)
}

// timeoutWaitingError formats the "timed out waiting for <resource>" message,
// wrapping the last transient error and the context error. It is the single
// source of this message for both the inter-poll wait and the pre-call context
// guard in triggerReconciliationWithRetry.
func timeoutWaitingError(resourceDescription string, lastErr, ctxErr error) error {
	return fmt.Errorf(
		"timed out waiting for %s to be available (last error: %w): %w",
		resourceDescription,
		lastErr,
		ctxErr,
	)
}

// handleTransientError waits for the next retry or returns a timeout error.
func handleTransientError(
	waitCtx context.Context,
	ticker *time.Ticker,
	resourceDescription string,
	err error,
) error {
	select {
	case <-waitCtx.Done():
		return timeoutWaitingError(resourceDescription, err, waitCtx.Err())
	case <-ticker.C:
		return nil // Continue retry loop
	}
}

// triggerReconciliationWithRetry triggers reconciliation on a Flux resource with retry logic
// for handling transient API errors (e.g. resource not yet created in slow CI environments).
//
// A JSON merge patch is used instead of the traditional Get+Update approach.  Patches
// are applied atomically server-side, so they never produce 409 Conflict errors even
// when Flux controllers are concurrently updating the same resource status.  This
// prevents the retry loop from running for the full apiAvailabilityTimeout when the
// cluster has many HelmReleases or kustomizations.
//
// It returns the reconcile-request token (the value written to the
// reconcile.fluxcd.io/requestedAt annotation). Once the source controller
// processes the request it mirrors this token into status.lastHandledReconcileAt,
// so a caller can wait for its own request to be handled — rather than trusting a
// stale Ready condition from a prior reconcile (see WaitForOCIRepositoryReady).
func triggerReconciliationWithRetry(
	ctx context.Context,
	client dynamic.ResourceInterface,
	resourceName string,
	resourceDescription string,
) (string, error) {
	// Create a timeout context for the entire retry operation.
	waitCtx, cancel := context.WithTimeout(ctx, apiAvailabilityTimeout)
	defer cancel()

	ticker := time.NewTicker(apiAvailabilityPollInterval)
	defer ticker.Stop()

	// Build the merge patch once; the timestamp is set at trigger time. The token
	// is returned so callers can match it against status.lastHandledReconcileAt.
	token := time.Now().Format(time.RFC3339Nano)
	patch := fmt.Appendf(nil,
		`{"metadata":{"annotations":{%q:%q}}}`,
		reconcileAnnotationKey,
		token,
	)

	var lastErr error

	for {
		// Guard against an expired context before making an API call.
		// Without this check, an expired context causes the k8s rate limiter to
		// return "rate: Wait(n=1) would exceed context deadline" — a plain
		// fmt.Errorf that does not wrap context.DeadlineExceeded and produces
		// confusing user-facing error messages if it bubbles up unhandled.
		err := waitCtx.Err()
		if err != nil {
			if lastErr != nil {
				return "", timeoutWaitingError(resourceDescription, lastErr, err)
			}

			return "", fmt.Errorf("trigger %s reconciliation: %w", resourceDescription, err)
		}

		_, err = client.Patch(
			waitCtx,
			resourceName,
			types.MergePatchType,
			patch,
			metav1.PatchOptions{},
		)
		if err == nil {
			return token, nil
		}

		if isTransientAPIError(err) {
			lastErr = err

			retryErr := handleTransientError(waitCtx, ticker, resourceDescription, lastErr)
			if retryErr != nil {
				return "", retryErr
			}

			continue
		}

		return "", fmt.Errorf("trigger %s reconciliation: %w", resourceDescription, err)
	}
}
