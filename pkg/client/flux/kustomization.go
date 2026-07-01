package flux

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/client/reconciler"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
)

// Kustomization constants.
const rootKustomizationName = "flux-system"

// ReconcileExcludeAnnotation is the KSail annotation key that, when set to
// "true" on a Flux Kustomization CR, excludes that kustomization from
// KSail's progress monitoring and readiness polling during
// `ksail workload reconcile`. Flux still reconciles the resource; only
// KSail's waiting phase is skipped.
const ReconcileExcludeAnnotation = "ksail.devantler.tech/reconcile-exclude"

// TriggerKustomizationReconciliation triggers Kustomization reconciliation without waiting.
// It uses a JSON merge patch with retry logic for transient API errors (e.g. resource not
// yet created, API server temporarily unavailable).
func (r *Reconciler) TriggerKustomizationReconciliation(ctx context.Context) error {
	return triggerReconciliationWithRetry(
		ctx,
		r.kustomizationClient(),
		rootKustomizationName,
		"flux kustomization",
	)
}

// TriggerNamedKustomizationReconciliation triggers reconciliation for a specific
// Kustomization CR identified by name. It uses the same retry logic as
// TriggerKustomizationReconciliation.
func (r *Reconciler) TriggerNamedKustomizationReconciliation(
	ctx context.Context,
	name string,
) error {
	return triggerReconciliationWithRetry(
		ctx,
		r.kustomizationClient(),
		name,
		"flux kustomization "+name,
	)
}

// KustomizationInfo holds the name, spec.path, and dependency information
// of a Flux Kustomization CR.
type KustomizationInfo struct {
	Name      string
	Path      string
	DependsOn []string
	// Excluded is true when the kustomization carries the
	// ReconcileExcludeAnnotation set to "true".
	Excluded bool
}

// ListKustomizations lists all Flux Kustomization CRs in the default
// namespace and returns their names, spec.path values, and dependency information.
func (r *Reconciler) ListKustomizations(
	ctx context.Context,
) ([]KustomizationInfo, error) {
	client := r.kustomizationClient()

	list, err := client.List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list flux kustomizations: %w", err)
	}

	infos := make([]KustomizationInfo, 0, len(list.Items))

	for i := range list.Items {
		name := list.Items[i].GetName()
		path, _, _ := unstructured.NestedString(list.Items[i].Object, "spec", "path")
		dependsOn := parseDependsOn(&list.Items[i])
		excluded := strings.EqualFold(
			strings.TrimSpace(list.Items[i].GetAnnotations()[ReconcileExcludeAnnotation]),
			"true",
		)

		infos = append(infos, KustomizationInfo{
			Name:      name,
			Path:      path,
			DependsOn: dependsOn,
			Excluded:  excluded,
		})
	}

	return infos, nil
}

// CheckNamedKustomizationReady performs a single-poll readiness check for
// a specific Kustomization CR identified by name.
// Returns (ready, status, error) where status is a human-readable string.
func (r *Reconciler) CheckNamedKustomizationReady(
	ctx context.Context,
	name string,
) (bool, string, error) {
	client := r.kustomizationClient()

	kustomization, err := client.Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return false, "", fmt.Errorf("get flux kustomization %q: %w", name, err)
	}

	// Resolve the revision the source has just made available so readiness is
	// evaluated against the just-pushed artifact rather than a leftover Ready
	// condition from the previous revision. An empty revision (source missing,
	// unknown kind, no artifact) falls back to condition-only readiness.
	sourceRevision := r.resolveSourceRevision(ctx, kustomization)

	return checkKustomizationStatus(kustomization, sourceRevision)
}

// kustomizationClient returns a dynamic client for Flux Kustomizations.
func (r *Reconciler) kustomizationClient() dynamic.ResourceInterface {
	return r.Dynamic.Resource(KustomizationGVR()).Namespace(DefaultNamespace)
}

// parseDependsOn extracts dependency names from a Kustomization CR's
// spec.dependsOn field. Only same-namespace dependencies (namespace empty or
// equal to DefaultNamespace) are included; cross-namespace references are
// treated as external and excluded from topological ordering.
func parseDependsOn(kustomization *unstructured.Unstructured) []string {
	deps, found, _ := unstructured.NestedSlice(kustomization.Object, "spec", "dependsOn")
	if !found || len(deps) == 0 {
		return nil
	}

	names := make([]string, 0, len(deps))

	for _, dep := range deps {
		depMap, ok := dep.(map[string]any)
		if !ok {
			continue
		}

		name, _, _ := unstructured.NestedString(depMap, "name")
		if name == "" {
			continue
		}

		ns, _, _ := unstructured.NestedString(depMap, "namespace")
		if ns != "" && ns != DefaultNamespace {
			continue
		}

		names = append(names, name)
	}

	return names
}

// checkKustomizationStatus checks the kustomization status and returns ready state,
// a human-readable status string for debugging, and any permanent failure errors.
//
// When sourceRevision is non-empty, readiness is revision-aware: the Ready
// condition is only trusted once the Kustomization has attempted the current
// source revision, and success additionally requires that revision to have been
// applied. This prevents a leftover condition from the previous revision from
// producing a false pass/fail immediately after a source artifact is pushed.
func checkKustomizationStatus(
	kustomization *unstructured.Unstructured,
	sourceRevision string,
) (bool, string, error) {
	conditions := reconciler.ParseConditions(kustomization)
	if len(conditions) == 0 {
		return false, "no conditions yet", nil
	}

	// Check if the status is stale (observedGeneration < generation)
	if isStatusStale(kustomization) {
		generation, _, _ := unstructured.NestedInt64(kustomization.Object, "metadata", "generation")
		observed, _, _ := unstructured.NestedInt64(
			kustomization.Object, "status", "observedGeneration",
		)

		return false, fmt.Sprintf("waiting for controller (generation %d, observed %d)",
			generation, observed), nil
	}

	// Revision-aware gate: until the Kustomization has attempted the current
	// source revision, its Ready condition describes a prior revision and must
	// not be trusted (avoids both the false-fail on a stale permanent failure
	// and the false-pass on a stale Ready=True).
	if sourceRevision != "" {
		if status, observed := revisionObservedStatus(kustomization, sourceRevision); !observed {
			return false, status, nil
		}
	}

	ready, status, err := evaluateKustomizationConditions(conditions)

	// A Ready=True verdict is only authoritative once the current source
	// revision has actually been applied; otherwise the apply is still in
	// flight and we keep polling.
	if err == nil && ready && sourceRevision != "" {
		if applied := lastAppliedRevision(kustomization); applied != sourceRevision {
			return false, fmt.Sprintf(
				"waiting for revision %s to be applied (applied %s)",
				shortRevision(sourceRevision), shortRevision(applied),
			), nil
		}
	}

	return ready, status, err
}

// revisionObservedStatus reports whether the Kustomization has attempted to
// reconcile the given source revision (status.lastAttemptedRevision == target).
// When it has not, observed is false and the returned status explains what the
// check is waiting for. Once observed, the Ready condition may be evaluated.
func revisionObservedStatus(
	kustomization *unstructured.Unstructured,
	target string,
) (string, bool) {
	if lastAttemptedRevision(kustomization) == target {
		return "", true
	}

	return fmt.Sprintf(
		"waiting for revision %s to be reconciled (attempted %s)",
		shortRevision(target), shortRevision(lastAttemptedRevision(kustomization)),
	), false
}

// lastAttemptedRevision returns status.lastAttemptedRevision, the source
// revision Flux most recently tried to build/apply.
func lastAttemptedRevision(kustomization *unstructured.Unstructured) string {
	revision, _, _ := unstructured.NestedString(
		kustomization.Object, "status", "lastAttemptedRevision",
	)

	return revision
}

// lastAppliedRevision returns status.lastAppliedRevision, the source revision
// Flux most recently applied successfully.
func lastAppliedRevision(kustomization *unstructured.Unstructured) string {
	revision, _, _ := unstructured.NestedString(
		kustomization.Object, "status", "lastAppliedRevision",
	)

	return revision
}

// shortRevision trims a Flux revision string for human-readable status output,
// keeping enough of the digest to be recognisable. An empty revision renders as
// "<none>".
func shortRevision(revision string) string {
	if revision == "" {
		return "<none>"
	}

	const maxLen = 20
	if len(revision) <= maxLen {
		return revision
	}

	return revision[:maxLen] + "…"
}

// isStatusStale checks if the observed generation is behind the current generation.
func isStatusStale(kustomization *unstructured.Unstructured) bool {
	generation, _, _ := unstructured.NestedInt64(kustomization.Object, "metadata", "generation")
	observedGeneration, _, _ := unstructured.NestedInt64(
		kustomization.Object, "status", "observedGeneration",
	)

	return observedGeneration < generation
}

// evaluateKustomizationConditions processes conditions and returns readiness status.
func evaluateKustomizationConditions(
	conditions []reconciler.Condition,
) (bool, string, error) {
	// Permanent failure reasons for Flux Kustomization.
	// Note: DependencyNotReady is intentionally excluded — it is a transient state
	// that resolves once upstream kustomizations in the dependency chain become ready.
	// Dependency-cascade handling is expected to be implemented by the caller:
	// when an upstream kustomization fails permanently, all downstream dependents
	// should fail promptly instead of waiting for the timeout.
	permanentFailureReasons := []string{
		"ReconciliationFailed",
		"ValidationFailed",
		"ArtifactFailed",
		"BuildFailed",
		"HealthCheckFailed",
	}

	var readyStatus, readyReason, readyMessage string

	for _, cond := range conditions {
		if cond.Type == conditionTypeReady {
			readyStatus = cond.Status
			readyReason = cond.Reason
			readyMessage = cond.Message

			if cond.Status == conditionStatusTrue {
				return true, conditionTypeReady, nil
			}
		}

		// Check for Stalled condition which indicates a permanent failure.
		if cond.Type == conditionTypeStalled &&
			cond.Status == conditionStatusTrue {
			return false, "", fmt.Errorf(
				"%w: stalled - %s",
				ErrKustomizationFailed,
				cond.Message,
			)
		}
	}

	// If Ready=False, check for permanent failures vs transient states.
	if readyStatus == conditionStatusFalse {
		if slices.Contains(permanentFailureReasons, readyReason) {
			return false, "", fmt.Errorf(
				"%w: %s - %s",
				ErrKustomizationFailed, readyReason, readyMessage,
			)
		}

		// Other Ready=False states are transient, keep polling.
		return false, fmt.Sprintf("%s: %s", readyReason, readyMessage), nil
	}

	return false, "waiting for Ready condition", nil
}
