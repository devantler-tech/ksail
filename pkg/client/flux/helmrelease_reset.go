package flux

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/client/reconciler"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

// HelmRelease reset constants.
const (
	// helmReleaseSuspendDelay is the time the code waits after suspending
	// HelmReleases before resuming them. A fixed delay is used rather than
	// condition-polling because stuck HelmReleases (Failed/Stalled) already
	// have Reconciling=False, so polling for "not reconciling" would return
	// immediately before the helm-controller has had a chance to process the
	// suspend patch.
	helmReleaseSuspendDelay = 2 * time.Second
	// helmReleaseResumeTimeout is the deadline for the resume phase. It is
	// deliberately generous so that even a large batch of HelmReleases can be
	// resumed before the context expires.
	helmReleaseResumeTimeout = 30 * time.Second
)

// StuckHelmRelease holds the identity and failure details of a HelmRelease
// that is stuck in a non-recoverable state.
type StuckHelmRelease struct {
	Name      string
	Namespace string
	Reason    string
	Message   string
}

// ListStuckHelmReleases returns all HelmReleases across all namespaces that are
// stuck in a non-recoverable state. A HelmRelease is considered stuck when its
// Ready condition is False with a failure reason (e.g., InstallFailed,
// UpgradeFailed) or when the Stalled condition is True.
// Returns an empty list without error when the HelmRelease CRD is not installed.
func (r *Reconciler) ListStuckHelmReleases(
	ctx context.Context,
) ([]StuckHelmRelease, error) {
	client := r.Dynamic.Resource(HelmReleaseGVR())

	list, err := client.List(ctx, metav1.ListOptions{})
	if err != nil {
		// CRD not installed — no HelmReleases exist, nothing to reset.
		if isAPIDiscoveryError(err.Error()) {
			return nil, nil
		}

		return nil, fmt.Errorf("list helmreleases: %w", err)
	}

	var stuck []StuckHelmRelease

	for i := range list.Items {
		if release := checkHelmReleaseStuck(&list.Items[i]); release != nil {
			stuck = append(stuck, *release)
		}
	}

	return stuck, nil
}

// ResetStuckHelmReleases performs a suspend/resume cycle on the given
// HelmReleases to clear their failure status. Returns the number of
// HelmReleases that were successfully reset. Errors on individual releases
// are collected and returned as a joined error; the operation is best-effort.
//
// The resume phase uses a context detached from any deadline so that
// HelmReleases are always resumed even if the parent context has expired.
func (r *Reconciler) ResetStuckHelmReleases(
	ctx context.Context,
	releases []StuckHelmRelease,
) (int, error) {
	if len(releases) == 0 {
		return 0, nil
	}

	gvr := HelmReleaseGVR()

	// Phase 1: Suspend all stuck HelmReleases.
	suspendPatch := []byte(`{"spec":{"suspend":true}}`)

	suspended, suspendErrs := r.patchHelmReleases(ctx, gvr, releases, suspendPatch, "suspend")

	if len(suspended) == 0 {
		return 0, errors.Join(suspendErrs...)
	}

	// Wait for the helm-controller to observe the suspension. A fixed delay
	// is used because stuck HelmReleases (Failed/Stalled) already have
	// Reconciling=False, so polling for condition absence would return
	// immediately—before the controller has processed the suspend patch.
	time.Sleep(helmReleaseSuspendDelay)

	// Phase 2: Resume using a detached context so the resume always runs
	// even when the parent deadline has expired.
	resumeCtx, resumeCancel := context.WithTimeout(
		context.WithoutCancel(ctx), helmReleaseResumeTimeout,
	)
	defer resumeCancel()

	resumePatch := []byte(`{"spec":{"suspend":false}}`)

	resumed, resumeErrs := r.patchHelmReleases(resumeCtx, gvr, suspended, resumePatch, "resume")

	return len(resumed), errors.Join(append(suspendErrs, resumeErrs...)...)
}

// patchHelmReleases applies patch to each HelmRelease and returns the list of
// HelmReleases patched successfully along with any errors.
func (r *Reconciler) patchHelmReleases(
	ctx context.Context,
	gvr schema.GroupVersionResource,
	releases []StuckHelmRelease,
	patch []byte,
	action string,
) ([]StuckHelmRelease, []error) {
	var errs []error

	patched := make([]StuckHelmRelease, 0, len(releases))

	for _, helmRelease := range releases {
		nsClient := r.Dynamic.Resource(gvr).Namespace(helmRelease.Namespace)

		_, err := nsClient.Patch(
			ctx, helmRelease.Name, types.MergePatchType,
			patch, metav1.PatchOptions{},
		)
		if err != nil {
			errs = append(errs, fmt.Errorf(
				"%s %s/%s: %w", action, helmRelease.Namespace, helmRelease.Name, err,
			))

			continue
		}

		patched = append(patched, helmRelease)
	}

	return patched, errs
}

// checkHelmReleaseStuck evaluates a HelmRelease's conditions and returns a
// StuckHelmRelease if it is stuck, or nil if it is healthy/transient.
// HelmReleases with spec.suspend=true are always skipped.
func checkHelmReleaseStuck(helmRelease *unstructured.Unstructured) *StuckHelmRelease {
	// Skip intentionally suspended HelmReleases.
	suspended, _, _ := unstructured.NestedBool(helmRelease.Object, "spec", "suspend")
	if suspended {
		return nil
	}

	conditions := reconciler.ParseConditions(helmRelease)
	if len(conditions) == 0 {
		return nil
	}

	return evaluateHelmReleaseConditions(helmRelease, conditions)
}

// evaluateHelmReleaseConditions scans a HelmRelease's conditions and returns
// a StuckHelmRelease for the first stuck condition found, or nil if healthy.
func evaluateHelmReleaseConditions(
	helmRelease *unstructured.Unstructured,
	conditions []reconciler.Condition,
) *StuckHelmRelease {
	// stuckReasons lists Ready condition reasons that indicate the release is
	// stuck and won't recover without intervention. DependencyNotReady is
	// intentionally excluded — it is transient and resolves when upstream
	// dependencies become ready.
	stuckReasons := []string{
		"InstallFailed",
		"UpgradeFailed",
		"ReconciliationFailed",
		"TestFailed",
		"RollbackFailed",
		"UninstallFailed",
		"GetLastReleaseFailed",
	}

	for _, cond := range conditions {
		if result := evaluateHelmReleaseCondition(helmRelease, cond, stuckReasons); result != nil {
			return result
		}
	}

	return nil
}

// evaluateHelmReleaseCondition checks a single condition and returns a
// StuckHelmRelease if the condition indicates a stuck state, or nil otherwise.
func evaluateHelmReleaseCondition(
	helmRelease *unstructured.Unstructured,
	cond reconciler.Condition,
	stuckReasons []string,
) *StuckHelmRelease {
	// Stalled=True means the controller has given up retrying.
	if cond.Type == conditionTypeStalled &&
		cond.Status == conditionStatusTrue {
		return &StuckHelmRelease{
			Name:      helmRelease.GetName(),
			Namespace: helmRelease.GetNamespace(),
			Reason:    cond.Reason,
			Message:   cond.Message,
		}
	}

	// Ready=False with a failure reason.
	if cond.Type == conditionTypeReady &&
		cond.Status == conditionStatusFalse &&
		slices.Contains(stuckReasons, cond.Reason) {
		return &StuckHelmRelease{
			Name:      helmRelease.GetName(),
			Namespace: helmRelease.GetNamespace(),
			Reason:    cond.Reason,
			Message:   cond.Message,
		}
	}

	return nil
}
