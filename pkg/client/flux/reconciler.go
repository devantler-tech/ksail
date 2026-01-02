package flux

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/k8s"
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
)

// Reconciler constants.
const (
	rootKustomizationName     = "flux-system"
	rootOCIRepositoryName     = "flux-system"
	ociRepositoryReadyTimeout = 2 * time.Minute
	pollInterval              = 2 * time.Second
	reconcileAnnotationKey    = "reconcile.fluxcd.io/requestedAt"
)

// Reconciler handles Flux reconciliation operations.
type Reconciler struct {
	dynamic        dynamic.Interface
	kubeconfigPath string
}

// NewReconciler creates a new Flux reconciler from kubeconfig path.
func NewReconciler(kubeconfigPath string) (*Reconciler, error) {
	restConfig, err := k8s.BuildRESTConfig(kubeconfigPath, "")
	if err != nil {
		return nil, fmt.Errorf("build rest config: %w", err)
	}

	dynamicClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("create dynamic client: %w", err)
	}

	return &Reconciler{
		dynamic:        dynamicClient,
		kubeconfigPath: kubeconfigPath,
	}, nil
}

// NewReconcilerWithClient creates a Reconciler with a provided dynamic client (for testing).
func NewReconcilerWithClient(dynamicClient dynamic.Interface) *Reconciler {
	return &Reconciler{dynamic: dynamicClient}
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
func (r *Reconciler) TriggerOCIRepositoryReconciliation(ctx context.Context) error {
	ociRepoClient := r.ociRepositoryClient()

	ociRepo, err := ociRepoClient.Get(ctx, rootOCIRepositoryName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get flux oci repository: %w", err)
	}

	annotations := ociRepo.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}

	annotations[reconcileAnnotationKey] = time.Now().Format(time.RFC3339Nano)
	ociRepo.SetAnnotations(annotations)

	_, err = ociRepoClient.Update(ctx, ociRepo, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("trigger flux oci repository reconciliation: %w", err)
	}

	return nil
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
func (r *Reconciler) TriggerKustomizationReconciliation(ctx context.Context) error {
	kustomizationClient := r.kustomizationClient()

	kustomization, err := kustomizationClient.Get(ctx, rootKustomizationName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get flux kustomization: %w", err)
	}

	annotations := kustomization.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}

	annotations[reconcileAnnotationKey] = time.Now().Format(time.RFC3339Nano)
	kustomization.SetAnnotations(annotations)

	_, err = kustomizationClient.Update(ctx, kustomization, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("trigger flux reconciliation: %w", err)
	}

	return nil
}

// WaitForKustomizationReady waits for the Kustomization to be ready.
func (r *Reconciler) WaitForKustomizationReady(ctx context.Context, timeout time.Duration) error {
	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	kustomizationClient := r.kustomizationClient()

	for {
		select {
		case <-timeoutCtx.Done():
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

			if isKustomizationReady(kustomization) {
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
//
//nolint:ireturn // dynamic.ResourceInterface is needed for Kubernetes dynamic client operations
func (r *Reconciler) ociRepositoryClient() dynamic.ResourceInterface {
	gvr := schema.GroupVersionResource{
		Group:    "source.toolkit.fluxcd.io",
		Version:  "v1",
		Resource: "ocirepositories",
	}

	return r.dynamic.Resource(gvr).Namespace(DefaultNamespace)
}

// kustomizationClient returns a dynamic client for Flux Kustomizations.
//
//nolint:ireturn // dynamic.ResourceInterface is needed for Kubernetes dynamic client operations
func (r *Reconciler) kustomizationClient() dynamic.ResourceInterface {
	gvr := schema.GroupVersionResource{
		Group:    "kustomize.toolkit.fluxcd.io",
		Version:  "v1",
		Resource: "kustomizations",
	}

	return r.dynamic.Resource(gvr).Namespace(DefaultNamespace)
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
		if condType != "Ready" {
			continue
		}

		condStatus, _, _ := unstructured.NestedString(condMap, "status")
		if condStatus == "True" {
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

// isKustomizationReady checks if the kustomization has Ready=True condition.
func isKustomizationReady(kustomization *unstructured.Unstructured) bool {
	conditions, found, err := unstructured.NestedSlice(kustomization.Object, "status", "conditions")
	if err != nil || !found {
		return false
	}

	for _, condition := range conditions {
		condMap, ok := condition.(map[string]any)
		if !ok {
			continue
		}

		condType, _, _ := unstructured.NestedString(condMap, "type")
		condStatus, _, _ := unstructured.NestedString(condMap, "status")

		if condType == "Ready" && condStatus == "True" {
			return true
		}
	}

	return false
}
