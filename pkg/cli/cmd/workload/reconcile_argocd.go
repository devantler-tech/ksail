package workload

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	v1alpha1 "github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/client/argocd"
	"github.com/devantler-tech/ksail/v5/pkg/utils/timer"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

// ArgoCD-specific errors.
var (
	errArgoCDReconcileTimeout   = errors.New("timeout waiting for argocd application sync")
	errArgoCDSourceNotAvailable = errors.New(
		"argocd application source is not available - ensure you have pushed an artifact with 'ksail workload push'",
	)
)

// ArgoCD-specific constants.
const (
	argoCDNamespace           = "argocd"
	argoCDRootApplicationName = "ksail"
)

// reconcileArgoCDApplication refreshes and waits for the ArgoCD application to sync.
func reconcileArgoCDApplication(
	ctx context.Context,
	clusterCfg *v1alpha1.Cluster,
	artifactVersion string,
	timeout time.Duration,
	outputTimer timer.Timer,
	writer io.Writer,
) error {
	kubeconfigPath, err := getKubeconfigPath(clusterCfg)
	if err != nil {
		return err
	}

	// Trigger hard refresh to fetch the latest artifact
	writeActivityNotification("triggering argocd application refresh", outputTimer, writer)

	err = triggerArgoCDRefresh(ctx, kubeconfigPath, artifactVersion)
	if err != nil {
		return err
	}

	applicationClient, err := getArgoCDApplicationClient(kubeconfigPath)
	if err != nil {
		return err
	}

	// Wait for application to sync - fails early if source is not available
	writeActivityNotification("waiting for argocd application to sync", outputTimer, writer)

	return waitForArgoCDApplicationReady(ctx, applicationClient, timeout)
}

// triggerArgoCDRefresh triggers a hard refresh of the ArgoCD application.
func triggerArgoCDRefresh(
	ctx context.Context,
	kubeconfigPath string,
	artifactVersion string,
) error {
	argocdMgr, err := argocd.NewManagerFromKubeconfig(kubeconfigPath)
	if err != nil {
		return fmt.Errorf("create argocd manager: %w", err)
	}

	err = argocdMgr.UpdateTargetRevision(ctx, argocd.UpdateTargetRevisionOptions{
		TargetRevision: artifactVersion,
		HardRefresh:    true,
	})
	if err != nil {
		return fmt.Errorf("refresh argocd application: %w", err)
	}

	return nil
}

// getArgoCDApplicationClient creates a dynamic client for ArgoCD applications.
//
//nolint:ireturn // dynamic.ResourceInterface is needed for Kubernetes dynamic client operations
func getArgoCDApplicationClient(kubeconfigPath string) (dynamic.ResourceInterface, error) {
	applicationGVR := schema.GroupVersionResource{
		Group:    "argoproj.io",
		Version:  "v1alpha1",
		Resource: "applications",
	}

	return getDynamicResourceClient(kubeconfigPath, applicationGVR, argoCDNamespace)
}

// checkArgoCDOperationState checks the operationState for source fetch errors.
func checkArgoCDOperationState(app *unstructured.Unstructured) error {
	operationState, found, _ := unstructured.NestedMap(app.Object, "status", "operationState")
	if !found {
		return nil
	}

	phase, _, _ := unstructured.NestedString(operationState, "phase")
	if phase != "Error" && phase != "Failed" {
		return nil
	}

	message, _, _ := unstructured.NestedString(operationState, "message")
	if isSourceRelatedError(message) {
		return fmt.Errorf("%w: %s", errArgoCDSourceNotAvailable, message)
	}

	return nil
}

// checkArgoCDConditions checks the conditions for repository errors.
func checkArgoCDConditions(app *unstructured.Unstructured) error {
	conditions, found, _ := unstructured.NestedSlice(app.Object, "status", "conditions")
	if !found {
		return nil
	}

	for _, condition := range conditions {
		condMap, ok := condition.(map[string]any)
		if !ok {
			continue
		}

		condType, _, _ := unstructured.NestedString(condMap, "type")
		if condType != "ComparisonError" && condType != "InvalidSpecError" {
			continue
		}

		condMessage, _, _ := unstructured.NestedString(condMap, "message")
		if isSourceRelatedError(condMessage) {
			return fmt.Errorf("%w: %s", errArgoCDSourceNotAvailable, condMessage)
		}
	}

	return nil
}

// isSourceRelatedError checks if an error message indicates a source/repository issue.
func isSourceRelatedError(message string) bool {
	return strings.Contains(message, "OCI") ||
		strings.Contains(message, "repository") ||
		strings.Contains(message, "manifest") ||
		strings.Contains(message, "not found")
}

// waitForArgoCDApplicationReady waits for the application to sync, failing early on source errors.
func waitForArgoCDApplicationReady(
	ctx context.Context,
	applicationClient dynamic.ResourceInterface,
	timeout time.Duration,
) error {
	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(reconcilePollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-timeoutCtx.Done():
			return errArgoCDReconcileTimeout
		case <-ticker.C:
			ready, err := checkArgoCDApplicationStatus(timeoutCtx, applicationClient)
			if err != nil {
				return err
			}

			if ready {
				return nil
			}
		}
	}
}

// checkArgoCDApplicationStatus checks if the ArgoCD application is ready or has errors.
// Returns (true, nil) if synced and healthy, (false, nil) if still progressing,
// or (false, error) if there's a source-related error.
func checkArgoCDApplicationStatus(
	ctx context.Context,
	applicationClient dynamic.ResourceInterface,
) (bool, error) {
	app, err := applicationClient.Get(ctx, argoCDRootApplicationName, metav1.GetOptions{})
	if err != nil {
		return false, fmt.Errorf("get argocd application status: %w", err)
	}

	// Check for source errors - fail early if artifact doesn't exist
	sourceErr := checkArgoCDOperationState(app)
	if sourceErr != nil {
		return false, sourceErr
	}

	sourceErr = checkArgoCDConditions(app)
	if sourceErr != nil {
		return false, sourceErr
	}

	// Check if synced and healthy
	return isArgoCDApplicationSynced(app), nil
}

// isArgoCDApplicationSynced checks if the application is synced and healthy.
func isArgoCDApplicationSynced(app *unstructured.Unstructured) bool {
	syncStatus, found, err := unstructured.NestedString(app.Object, "status", "sync", "status")
	if err != nil || !found {
		return false
	}

	healthStatus, found, err := unstructured.NestedString(app.Object, "status", "health", "status")
	if err != nil || !found {
		return false
	}

	return syncStatus == "Synced" && healthStatus == "Healthy"
}
