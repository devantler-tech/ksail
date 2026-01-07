package flux

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/svc/reconciler"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

// Reconciler errors.
var (
	// ErrReconcileTimeout is returned when reconciliation times out.
	ErrReconcileTimeout = errors.New("timeout waiting for flux kustomization reconciliation")
	// ErrOCIRepositoryNotReady is returned when the OCIRepository is not ready.
	ErrOCIRepositoryNotReady = errors.New(
		"flux OCIRepository is not ready - ensure you have pushed an artifact with 'ksail workload push'",
	)
	// ErrKustomizationFailed is returned when the Kustomization reconciliation fails.
	ErrKustomizationFailed = errors.New("flux kustomization reconciliation failed")
)

// Reconciler constants.
const (
	rootKustomizationName     = "flux-system"
	rootOCIRepositoryName     = "flux-system"
	ociRepositoryReadyTimeout = 2 * time.Minute
	pollInterval              = 2 * time.Second
	reconcileAnnotationKey    = "reconcile.fluxcd.io/requestedAt"

	// Condition type and status constants.
	conditionTypeReady   = "Ready"
	conditionTypeStalled = "Stalled"
	conditionStatusTrue  = "True"
	conditionStatusFalse = "False"

	// Retry constants for handling optimistic concurrency conflicts.
	conflictRetryAttempts = 5
	conflictRetryDelay    = 500 * time.Millisecond
)

// Reconciler handles Flux reconciliation operations.
type Reconciler struct {
	*reconciler.Base
}

// newFromBase creates a Reconciler from a base reconciler.
func newFromBase(base *reconciler.Base) *Reconciler {
	return &Reconciler{Base: base}
}

// NewReconciler creates a new Flux reconciler from kubeconfig path.
func NewReconciler(kubeconfigPath string) (*Reconciler, error) {
	r, err := reconciler.New(kubeconfigPath, newFromBase)
	if err != nil {
		return nil, fmt.Errorf("create flux reconciler: %w", err)
	}

	return r, nil
}

// NewReconcilerWithClient creates a Reconciler with a provided dynamic client (for testing).
func NewReconcilerWithClient(dynamicClient dynamic.Interface) *Reconciler {
	return reconciler.NewWithClient(dynamicClient, newFromBase)
}

// ReconcileOptions configures the reconciliation behavior.
type ReconcileOptions struct {
	// Timeout for waiting for kustomization reconciliation.
	Timeout time.Duration
}

// Reconcile triggers and waits for Flux kustomization reconciliation.
// It first reconciles the OCIRepository to fetch the latest artifact,
// then reconciles the Kustomization to apply the changes.
func (r *Reconciler) Reconcile(ctx context.Context, opts ReconcileOptions) error {
	// First reconcile the OCIRepository to fetch the latest artifact
	err := r.reconcileOCIRepository(ctx)
	if err != nil {
		return err
	}

	// Then reconcile the Kustomization to apply the changes
	return r.reconcileKustomization(ctx, opts.Timeout)
}

// TriggerOCIRepositoryReconciliation triggers OCIRepository reconciliation without waiting.
// It uses retry logic to handle optimistic concurrency conflicts that can occur when the
// Flux controller updates the resource between our Get and Update calls.
func (r *Reconciler) TriggerOCIRepositoryReconciliation(ctx context.Context) error {
	return triggerReconciliationWithRetry(
		ctx,
		r.ociRepositoryClient(),
		rootOCIRepositoryName,
		"flux oci repository",
	)
}

// WaitForOCIRepositoryReady waits for the OCIRepository to be ready.
func (r *Reconciler) WaitForOCIRepositoryReady(ctx context.Context) error {
	timeoutCtx, cancel := context.WithTimeout(ctx, ociRepositoryReadyTimeout)
	defer cancel()

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	var lastErr error

	ociRepoClient := r.ociRepositoryClient()

	for {
		select {
		case <-timeoutCtx.Done():
			if lastErr != nil {
				return lastErr
			}

			return ErrOCIRepositoryNotReady
		case <-ticker.C:
			ready, err := r.checkOCIRepositoryStatus(timeoutCtx, ociRepoClient)
			if err != nil {
				if isPermanentOCIError(err) {
					return err
				}

				lastErr = err

				continue
			}

			if ready {
				return nil
			}
		}
	}
}

// TriggerKustomizationReconciliation triggers Kustomization reconciliation without waiting.
// It uses retry logic to handle optimistic concurrency conflicts that can occur when the
// Flux controller updates the resource between our Get and Update calls.
func (r *Reconciler) TriggerKustomizationReconciliation(ctx context.Context) error {
	return triggerReconciliationWithRetry(
		ctx,
		r.kustomizationClient(),
		rootKustomizationName,
		"flux kustomization",
	)
}

// WaitForKustomizationReady waits for the Kustomization to be ready.
func (r *Reconciler) WaitForKustomizationReady(ctx context.Context, timeout time.Duration) error {
	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	kustomizationClient := r.kustomizationClient()

	var lastStatus string

	for {
		select {
		case <-timeoutCtx.Done():
			if lastStatus != "" {
				return fmt.Errorf("%w: last status: %s", ErrReconcileTimeout, lastStatus)
			}

			return ErrReconcileTimeout
		case <-ticker.C:
			kustomization, err := kustomizationClient.Get(
				timeoutCtx,
				rootKustomizationName,
				metav1.GetOptions{},
			)
			if err != nil {
				return fmt.Errorf("get flux kustomization status: %w", err)
			}

			ready, status, err := checkKustomizationStatus(kustomization)
			if err != nil {
				return err
			}

			lastStatus = status

			if ready {
				return nil
			}
		}
	}
}

// reconcileOCIRepository triggers and waits for OCIRepository reconciliation.
func (r *Reconciler) reconcileOCIRepository(ctx context.Context) error {
	err := r.TriggerOCIRepositoryReconciliation(ctx)
	if err != nil {
		return err
	}

	return r.WaitForOCIRepositoryReady(ctx)
}

// reconcileKustomization triggers and waits for Kustomization reconciliation.
func (r *Reconciler) reconcileKustomization(ctx context.Context, timeout time.Duration) error {
	err := r.TriggerKustomizationReconciliation(ctx)
	if err != nil {
		return err
	}

	return r.WaitForKustomizationReady(ctx, timeout)
}

// ociRepositoryClient returns a dynamic client for Flux OCIRepositories.
func (r *Reconciler) ociRepositoryClient() dynamic.ResourceInterface {
	gvr := schema.GroupVersionResource{
		Group:    "source.toolkit.fluxcd.io",
		Version:  "v1",
		Resource: "ocirepositories",
	}

	return r.Dynamic.Resource(gvr).Namespace(DefaultNamespace)
}

// kustomizationClient returns a dynamic client for Flux Kustomizations.
func (r *Reconciler) kustomizationClient() dynamic.ResourceInterface {
	gvr := schema.GroupVersionResource{
		Group:    "kustomize.toolkit.fluxcd.io",
		Version:  "v1",
		Resource: "kustomizations",
	}

	return r.Dynamic.Resource(gvr).Namespace(DefaultNamespace)
}

// checkOCIRepositoryStatus checks if the OCIRepository has successfully fetched an artifact.
func (r *Reconciler) checkOCIRepositoryStatus(
	ctx context.Context,
	client dynamic.ResourceInterface,
) (bool, error) {
	ociRepo, err := client.Get(ctx, rootOCIRepositoryName, metav1.GetOptions{})
	if err != nil {
		return false, fmt.Errorf("get flux oci repository: %w", err)
	}

	conditions, found, _ := unstructured.NestedSlice(ociRepo.Object, "status", "conditions")
	if !found || len(conditions) == 0 {
		return false, nil // Still progressing, no conditions yet
	}

	return evaluateOCIRepositoryConditions(conditions)
}

// evaluateOCIRepositoryConditions evaluates conditions to determine readiness.
func evaluateOCIRepositoryConditions(conditions []any) (bool, error) {
	for _, condition := range conditions {
		condMap, ok := condition.(map[string]any)
		if !ok {
			continue
		}

		condType, _, _ := unstructured.NestedString(condMap, "type")
		if condType != conditionTypeReady {
			continue
		}

		condStatus, _, _ := unstructured.NestedString(condMap, "status")
		if condStatus == conditionStatusTrue {
			return true, nil
		}

		condReason, _, _ := unstructured.NestedString(condMap, "reason")
		condMessage, _, _ := unstructured.NestedString(condMap, "message")

		// Check for permanent failures that indicate the artifact doesn't exist
		if condReason == "OCIPullFailed" || condReason == "OCIArtifactPullFailed" {
			return false, fmt.Errorf("%w: %s", ErrOCIRepositoryNotReady, condMessage)
		}

		// For other non-ready states, keep waiting
		return false, nil
	}

	return false, nil // No Ready condition found, still progressing
}

// isPermanentOCIError checks if an error indicates a permanent failure.
func isPermanentOCIError(err error) bool {
	if err == nil {
		return false
	}

	errMsg := err.Error()

	return strings.Contains(errMsg, "manifest unknown") ||
		strings.Contains(errMsg, "not found") ||
		strings.Contains(errMsg, "does not exist")
}

// isConflictError checks if an error is an optimistic concurrency conflict.
// This happens when the resource was modified between our Get and Update calls.
func isConflictError(err error) bool {
	return apierrors.IsConflict(err)
}

// triggerReconciliationWithRetry triggers reconciliation on a Flux resource with retry logic
// for handling optimistic concurrency conflicts.
func triggerReconciliationWithRetry(
	ctx context.Context,
	client dynamic.ResourceInterface,
	resourceName string,
	resourceDescription string,
) error {
	var lastErr error

	for attempt := range conflictRetryAttempts {
		resource, err := client.Get(ctx, resourceName, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("get %s: %w", resourceDescription, err)
		}

		annotations := resource.GetAnnotations()
		if annotations == nil {
			annotations = make(map[string]string)
		}

		annotations[reconcileAnnotationKey] = time.Now().Format(time.RFC3339Nano)
		resource.SetAnnotations(annotations)

		_, err = client.Update(ctx, resource, metav1.UpdateOptions{})
		if err == nil {
			return nil // Success
		}

		// Check if this is a conflict error (resource version mismatch)
		if isConflictError(err) {
			lastErr = err

			if attempt < conflictRetryAttempts-1 {
				time.Sleep(conflictRetryDelay)

				continue
			}
		}

		// Non-conflict error or final attempt
		return fmt.Errorf("trigger %s reconciliation: %w", resourceDescription, err)
	}

	return fmt.Errorf(
		"trigger %s reconciliation after %d retries: %w",
		resourceDescription,
		conflictRetryAttempts,
		lastErr,
	)
}

// checkKustomizationStatus checks the kustomization status and returns ready state,
// a human-readable status string for debugging, and any permanent failure errors.
func checkKustomizationStatus(
	kustomization *unstructured.Unstructured,
) (bool, string, error) {
	conditions, found, _ := unstructured.NestedSlice(kustomization.Object, "status", "conditions")
	if !found || len(conditions) == 0 {
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

	return evaluateKustomizationConditions(conditions)
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
func evaluateKustomizationConditions(conditions []any) (bool, string, error) {
	// Permanent failure reasons for Flux Kustomization.
	permanentFailureReasons := []string{
		"ReconciliationFailed",
		"ValidationFailed",
		"DependencyNotReady",
		"ArtifactFailed",
	}

	var readyStatus, readyReason, readyMessage string

	for _, condition := range conditions {
		condMap, ok := condition.(map[string]any)
		if !ok {
			continue
		}

		condType, _, _ := unstructured.NestedString(condMap, "type")
		condStatus, _, _ := unstructured.NestedString(condMap, "status")
		condReason, _, _ := unstructured.NestedString(condMap, "reason")
		condMessage, _, _ := unstructured.NestedString(condMap, "message")

		if condType == conditionTypeReady {
			readyStatus = condStatus
			readyReason = condReason
			readyMessage = condMessage

			if condStatus == conditionStatusTrue {
				return true, conditionTypeReady, nil
			}
		}

		// Check for Stalled condition which indicates a permanent failure.
		if condType == conditionTypeStalled && condStatus == conditionStatusTrue {
			return false, "", fmt.Errorf("%w: stalled - %s", ErrKustomizationFailed, condMessage)
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
