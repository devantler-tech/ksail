package flux

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/client/reconciler"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
)

// OCIRepository constants.
const (
	rootOCIRepositoryName     = "flux-system"
	ociRepositoryReadyTimeout = 2 * time.Minute
	pollInterval              = 500 * time.Millisecond

	// OCI substrings used to detect that an artifact does not exist.
	ociErrManifestUnknownSubstr = "manifest unknown"
	ociErrDoesNotExistSubstr    = "does not exist"
)

// TriggerOCIRepositoryReconciliation triggers OCIRepository reconciliation without waiting.
// It uses a JSON merge patch with retry logic for transient API errors (e.g. resource not
// yet created, API server temporarily unavailable).
//
// It returns the reconcile-request token written to the resource; pass it to
// WaitForOCIRepositoryReady so the wait gates on this request being handled (via
// status.lastHandledReconcileAt) rather than trusting a stale Ready condition.
func (r *Reconciler) TriggerOCIRepositoryReconciliation(ctx context.Context) (string, error) {
	return triggerReconciliationWithRetry(
		ctx,
		r.ociRepositoryClient(),
		rootOCIRepositoryName,
		"flux oci repository",
	)
}

// WaitForOCIRepositoryReady waits for the OCIRepository to be ready.
// If timeout is zero or negative, the default ociRepositoryReadyTimeout is used.
//
// expectedReconcileToken is the value returned by
// TriggerOCIRepositoryReconciliation. When non-empty, readiness additionally
// requires that this reconcile request has been handled
// (status.lastHandledReconcileAt == token) AND has finished (no Reconciling=True
// condition) — proving the source controller has processed *this* request and
// served the just-pushed artifact, rather than reporting a Ready condition left
// over from a previous reconcile of a stale revision (bug #5717). An empty token
// preserves the prior condition-only behaviour for callers that did not just
// trigger.
func (r *Reconciler) WaitForOCIRepositoryReady(
	ctx context.Context,
	timeout time.Duration,
	expectedReconcileToken string,
) error {
	if timeout <= 0 {
		timeout = ociRepositoryReadyTimeout
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	var lastErr error

	ociRepoClient := r.ociRepositoryClient()

	for {
		ready, err := r.pollOCIRepositoryStatus(
			timeoutCtx, ociRepoClient, expectedReconcileToken, &lastErr,
		)
		if err != nil {
			return err
		}

		if ready {
			return nil
		}

		select {
		case <-timeoutCtx.Done():
			return ociTimeoutError(lastErr)
		case <-ticker.C:
		}
	}
}

// pollOCIRepositoryStatus checks OCI repository status with timeout guard.
// It returns (ready, nil) on success, (false, nil) for transient errors (stored in lastErr),
// or (false, err) for permanent/timeout errors.
func (r *Reconciler) pollOCIRepositoryStatus(
	ctx context.Context,
	client dynamic.ResourceInterface,
	expectedReconcileToken string,
	lastErr *error,
) (bool, error) {
	err := ctx.Err()
	if err != nil {
		return false, ociTimeoutError(*lastErr)
	}

	ready, err := r.checkOCIRepositoryStatus(ctx, client, expectedReconcileToken)
	if err != nil {
		if isPermanentOCIError(err) {
			return false, err
		}

		if reconciler.IsContextError(err) {
			return false, ociTimeoutError(*lastErr)
		}

		*lastErr = err

		return false, nil
	}

	return ready, nil
}

// ociTimeoutError returns lastErr if available, otherwise ErrOCIRepositoryNotReady.
func ociTimeoutError(lastErr error) error {
	if lastErr != nil {
		return lastErr
	}

	return ErrOCIRepositoryNotReady
}

// ociRepositoryClient returns a dynamic client for Flux OCIRepositories.
func (r *Reconciler) ociRepositoryClient() dynamic.ResourceInterface {
	return r.Dynamic.Resource(OCIRepositoryGVR()).Namespace(DefaultNamespace)
}

// checkOCIRepositoryStatus checks if the OCIRepository has successfully fetched an artifact.
//
// When expectedReconcileToken is non-empty the resource must first have handled
// that specific reconcile request (status.lastHandledReconcileAt == token) AND
// have finished reconciling it (no Reconciling=True condition) before its Ready
// condition is trusted. status.lastHandledReconcileAt is only an *acknowledgement*
// — the source controller records it when it picks up the request but keeps the
// prior Ready=True until the reconcile completes, so gating on the token alone
// would accept a Ready left over from a previous reconcile of a stale revision
// before the just-pushed artifact is ingested (bug #5717). Requiring the
// Reconciling condition to have cleared mirrors `flux reconcile --wait`, which
// waits for the object to settle before reporting it ready.
func (r *Reconciler) checkOCIRepositoryStatus(
	ctx context.Context,
	client dynamic.ResourceInterface,
	expectedReconcileToken string,
) (bool, error) {
	ociRepo, err := client.Get(ctx, rootOCIRepositoryName, metav1.GetOptions{})
	if err != nil {
		return false, fmt.Errorf("get flux oci repository: %w", err)
	}

	if !reconcileRequestHandled(ociRepo, expectedReconcileToken) {
		return false, nil
	}

	conditions := reconciler.ParseConditions(ociRepo)

	// A handled request that is still reconciling means the controller has only
	// acknowledged our trigger, not finished serving the new artifact — keep
	// waiting rather than trusting the stale Ready (#5717). Only enforced when we
	// triggered the reconcile; the empty-token path keeps its condition-only
	// behaviour.
	if expectedReconcileToken != "" && reconcileInProgress(conditions) {
		return false, nil
	}

	return evaluateOCIRepositoryConditions(conditions)
}

// reconcileRequestHandled reports whether the resource's
// status.lastHandledReconcileAt matches the token we requested. An empty token
// (no fresh trigger) is always considered handled so the condition-only path is
// preserved.
func reconcileRequestHandled(
	resource *unstructured.Unstructured,
	expectedReconcileToken string,
) bool {
	if expectedReconcileToken == "" {
		return true
	}

	handled, _, _ := unstructured.NestedString(
		resource.Object, "status", "lastHandledReconcileAt",
	)

	return handled == expectedReconcileToken
}

// reconcileInProgress reports whether the resource carries a Reconciling=True
// condition — i.e. the source controller is mid-reconcile and any Ready
// condition still reflects the previous artifact. Flux adds this condition while
// reconciling and removes it once the object settles, so its absence (together
// with a handled reconcile token) marks the triggered reconcile as complete.
func reconcileInProgress(conditions []reconciler.Condition) bool {
	for _, cond := range conditions {
		if cond.Type == conditionTypeReconciling {
			return cond.Status == conditionStatusTrue
		}
	}

	return false
}

// evaluateOCIRepositoryConditions evaluates conditions to determine readiness.
func evaluateOCIRepositoryConditions(conditions []reconciler.Condition) (bool, error) {
	for _, cond := range conditions {
		if cond.Type != conditionTypeReady {
			continue
		}

		if cond.Status == conditionStatusTrue {
			return true, nil
		}

		// Check for permanent failures that indicate the artifact doesn't exist
		if cond.Reason == "OCIPullFailed" || cond.Reason == "OCIArtifactPullFailed" {
			return false, fmt.Errorf("%w: %s", ErrOCIRepositoryNotReady, cond.Message)
		}

		// For other non-ready states, keep waiting
		return false, nil
	}

	return false, nil // No Ready condition found, still progressing
}

// isPermanentOCIError checks if an error indicates a permanent failure.
// This distinguishes between OCI/artifact errors (permanent) and Kubernetes
// resource NotFound errors (transient - the resource may not exist yet).
func isPermanentOCIError(err error) bool {
	if err == nil {
		return false
	}

	// Kubernetes NotFound errors are transient - the OCIRepository may not
	// have been created yet by the Instance controller.
	if apierrors.IsNotFound(err) {
		return false
	}

	errMsg := err.Error()

	// OCI-specific errors that indicate the artifact doesn't exist
	return strings.Contains(errMsg, ociErrManifestUnknownSubstr) ||
		strings.Contains(errMsg, ociErrDoesNotExistSubstr)
}
