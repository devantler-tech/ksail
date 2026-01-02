package workload

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	v1alpha1 "github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/utils/timer"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

// Flux-specific errors.
var (
	errFluxReconcileTimeout = errors.New(
		"timeout waiting for flux kustomization reconciliation",
	)
	errFluxOCIRepositoryNotReady = errors.New(
		"flux OCIRepository is not ready - ensure you have pushed an artifact with 'ksail workload push'",
	)
)

// Flux-specific constants.
const (
	fluxNamespace                 = "flux-system"
	fluxRootKustomizationName     = "flux-system"
	fluxRootOCIRepositoryName     = "flux-system"
	ociRepositoryReadinessTimeout = 2 * time.Minute
)

// reconcileFluxKustomization triggers and waits for Flux kustomization reconciliation.
func reconcileFluxKustomization(
	ctx context.Context,
	clusterCfg *v1alpha1.Cluster,
	timeout time.Duration,
	outputTimer timer.Timer,
	writer io.Writer,
) error {
	kubeconfigPath, err := getKubeconfigPath(clusterCfg)
	if err != nil {
		return err
	}

	// First reconcile the OCIRepository to fetch the latest artifact
	err = reconcileFluxOCIRepository(ctx, kubeconfigPath, outputTimer, writer)
	if err != nil {
		return err
	}

	// Then reconcile the Kustomization to apply the changes
	return reconcileFluxKustomizationResource(ctx, kubeconfigPath, timeout, outputTimer, writer)
}

// reconcileFluxOCIRepository triggers and waits for the OCIRepository to be ready.
func reconcileFluxOCIRepository(
	ctx context.Context,
	kubeconfigPath string,
	outputTimer timer.Timer,
	writer io.Writer,
) error {
	ociRepoClient, err := getFluxOCIRepositoryClient(kubeconfigPath)
	if err != nil {
		return err
	}

	writeActivityNotification("triggering flux oci repository reconciliation", outputTimer, writer)

	err = triggerFluxOCIRepositoryReconciliation(ctx, ociRepoClient)
	if err != nil {
		return err
	}

	writeActivityNotification("waiting for flux oci repository to be ready", outputTimer, writer)

	return waitForFluxOCIRepositoryReady(ctx, ociRepoClient)
}

// reconcileFluxKustomizationResource triggers and waits for the Kustomization to be ready.
func reconcileFluxKustomizationResource(
	ctx context.Context,
	kubeconfigPath string,
	timeout time.Duration,
	outputTimer timer.Timer,
	writer io.Writer,
) error {
	kustomizationClient, err := getFluxKustomizationClient(kubeconfigPath)
	if err != nil {
		return err
	}

	writeActivityNotification("triggering flux kustomization reconciliation", outputTimer, writer)

	err = triggerFluxReconciliation(ctx, kustomizationClient)
	if err != nil {
		return err
	}

	writeActivityNotification("waiting for flux kustomization to reconcile", outputTimer, writer)

	return waitForFluxReconciliation(ctx, kustomizationClient, timeout)
}

// getFluxOCIRepositoryClient creates a dynamic client for Flux OCIRepositories.
//
//nolint:ireturn // dynamic.ResourceInterface is needed for Kubernetes dynamic client operations
func getFluxOCIRepositoryClient(
	kubeconfigPath string,
) (dynamic.ResourceInterface, error) {
	ociRepositoryGVR := schema.GroupVersionResource{
		Group:    "source.toolkit.fluxcd.io",
		Version:  "v1",
		Resource: "ocirepositories",
	}

	return getDynamicResourceClient(kubeconfigPath, ociRepositoryGVR, fluxNamespace)
}

// triggerFluxOCIRepositoryReconciliation annotates the OCIRepository to trigger reconciliation.
func triggerFluxOCIRepositoryReconciliation(
	ctx context.Context,
	ociRepoClient dynamic.ResourceInterface,
) error {
	ociRepo, err := ociRepoClient.Get(ctx, fluxRootOCIRepositoryName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get flux oci repository: %w", err)
	}

	annotations := ociRepo.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}

	annotations["reconcile.fluxcd.io/requestedAt"] = time.Now().Format(time.RFC3339Nano)
	ociRepo.SetAnnotations(annotations)

	_, err = ociRepoClient.Update(ctx, ociRepo, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("trigger flux oci repository reconciliation: %w", err)
	}

	return nil
}

// waitForFluxOCIRepositoryReady waits for the OCIRepository to be ready with a timeout.
// This is needed because after pushing an artifact, the OCIRepository needs time to fetch it.
func waitForFluxOCIRepositoryReady(
	ctx context.Context,
	ociRepoClient dynamic.ResourceInterface,
) error {
	timeoutCtx, cancel := context.WithTimeout(ctx, ociRepositoryReadinessTimeout)
	defer cancel()

	ticker := time.NewTicker(reconcilePollInterval)
	defer ticker.Stop()

	var lastErr error

	for {
		select {
		case <-timeoutCtx.Done():
			if lastErr != nil {
				return lastErr
			}

			return errFluxOCIRepositoryNotReady
		case <-ticker.C:
			ready, err := checkFluxOCIRepositoryStatus(timeoutCtx, ociRepoClient)
			if err != nil {
				// If it's a permanent error (like manifest not found), return immediately
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

// checkFluxOCIRepositoryStatus checks if the OCIRepository has successfully fetched an artifact.
// Returns (true, nil) if ready, (false, nil) if still progressing, or (false, error) on failure.
func checkFluxOCIRepositoryStatus(
	ctx context.Context,
	ociRepoClient dynamic.ResourceInterface,
) (bool, error) {
	ociRepo, err := ociRepoClient.Get(ctx, fluxRootOCIRepositoryName, metav1.GetOptions{})
	if err != nil {
		return false, fmt.Errorf("get flux oci repository: %w", err)
	}

	conditions, found, _ := unstructured.NestedSlice(ociRepo.Object, "status", "conditions")
	if !found || len(conditions) == 0 {
		return false, nil // Still progressing, no conditions yet
	}

	return evaluateFluxOCIRepositoryConditions(conditions)
}

// evaluateFluxOCIRepositoryConditions evaluates conditions to determine readiness.
// Returns (true, nil) if ready, (false, nil) if progressing, or (false, error) on failure.
func evaluateFluxOCIRepositoryConditions(conditions []any) (bool, error) {
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
			return false, fmt.Errorf("%w: %s", errFluxOCIRepositoryNotReady, condMessage)
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

// getFluxKustomizationClient creates a dynamic client for Flux kustomizations.
//
//nolint:ireturn // dynamic.ResourceInterface is needed for Kubernetes dynamic client operations
func getFluxKustomizationClient(
	kubeconfigPath string,
) (dynamic.ResourceInterface, error) {
	kustomizationGVR := schema.GroupVersionResource{
		Group:    "kustomize.toolkit.fluxcd.io",
		Version:  "v1",
		Resource: "kustomizations",
	}

	return getDynamicResourceClient(kubeconfigPath, kustomizationGVR, fluxNamespace)
}

// triggerFluxReconciliation annotates the kustomization to trigger reconciliation.
func triggerFluxReconciliation(
	ctx context.Context,
	kustomizationClient dynamic.ResourceInterface,
) error {
	kustomization, err := kustomizationClient.Get(
		ctx,
		fluxRootKustomizationName,
		metav1.GetOptions{},
	)
	if err != nil {
		return fmt.Errorf("get flux kustomization: %w", err)
	}

	annotations := kustomization.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}

	annotations["reconcile.fluxcd.io/requestedAt"] = time.Now().Format(time.RFC3339Nano)
	kustomization.SetAnnotations(annotations)

	_, err = kustomizationClient.Update(ctx, kustomization, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("trigger flux reconciliation: %w", err)
	}

	return nil
}

// waitForFluxReconciliation waits for the kustomization to become ready.
func waitForFluxReconciliation(
	ctx context.Context,
	kustomizationClient dynamic.ResourceInterface,
	timeout time.Duration,
) error {
	return waitForResourceCondition(
		ctx,
		kustomizationClient,
		fluxRootKustomizationName,
		timeout,
		isFluxKustomizationReady,
		errFluxReconcileTimeout,
		"get flux kustomization status",
	)
}

// isFluxKustomizationReady checks if the kustomization has Ready=True condition.
func isFluxKustomizationReady(kustomization *unstructured.Unstructured) bool {
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
