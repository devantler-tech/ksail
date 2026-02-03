package argocd

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/svc/reconciler"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/util/retry"
)

// Reconciler errors.
var (
	// ErrReconcileTimeout is returned when reconciliation times out.
	ErrReconcileTimeout = errors.New("timeout waiting for argocd application sync")
	// ErrSourceNotAvailable is returned when the ArgoCD source is not available.
	ErrSourceNotAvailable = errors.New(
		"argocd source is not available - ensure you have pushed an artifact with 'ksail workload push'",
	)
	// ErrOperationFailed is returned when an ArgoCD operation fails.
	ErrOperationFailed = errors.New("argocd operation failed")
)

// Reconciler constants.
const (
	// DefaultNamespace is the default namespace for ArgoCD resources.
	DefaultNamespace       = "argocd"
	rootApplicationName    = "ksail"
	reconcilerPollInterval = 2 * time.Second
)

// Reconciler handles ArgoCD reconciliation operations.
type Reconciler struct {
	*reconciler.Base
}

// newFromBase creates a Reconciler from a base reconciler.
func newFromBase(base *reconciler.Base) *Reconciler {
	return &Reconciler{Base: base}
}

// NewReconciler creates a new ArgoCD reconciler from kubeconfig path.
func NewReconciler(kubeconfigPath string) (*Reconciler, error) {
	r, err := reconciler.New(kubeconfigPath, newFromBase)
	if err != nil {
		return nil, fmt.Errorf("create argocd reconciler: %w", err)
	}

	return r, nil
}

// NewReconcilerWithClient creates a Reconciler with a provided dynamic client (for testing).
func NewReconcilerWithClient(dynamicClient dynamic.Interface) *Reconciler {
	return reconciler.NewWithClient(dynamicClient, newFromBase)
}

// ReconcileOptions configures the reconciliation behavior.
type ReconcileOptions struct {
	// Timeout for waiting for application sync.
	Timeout time.Duration
	// HardRefresh requests ArgoCD to refresh caches.
	HardRefresh bool
}

// Reconcile triggers and waits for ArgoCD application sync.
func (r *Reconciler) Reconcile(ctx context.Context, opts ReconcileOptions) error {
	// First trigger the refresh
	err := r.TriggerRefresh(ctx, opts.HardRefresh)
	if err != nil {
		return err
	}

	// Then wait for the application to be synced
	return r.WaitForApplicationReady(ctx, opts.Timeout)
}

// TriggerRefresh triggers an ArgoCD application refresh.
// Uses retry logic to handle optimistic concurrency conflicts when the Application
// is modified by ArgoCD controllers between GET and UPDATE operations.
func (r *Reconciler) TriggerRefresh(ctx context.Context, hardRefresh bool) error {
	appClient := r.applicationClient()

	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		app, err := appClient.Get(ctx, rootApplicationName, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("get argocd application: %w", err)
		}

		annotations := app.GetAnnotations()
		if annotations == nil {
			annotations = make(map[string]string)
		}

		if hardRefresh {
			annotations[argoCDRefreshAnnotationKey] = argoCDHardRefreshAnnotation
		} else {
			annotations[argoCDRefreshAnnotationKey] = "normal"
		}

		app.SetAnnotations(annotations)

		_, err = appClient.Update(ctx, app, metav1.UpdateOptions{})
		if err != nil {
			return fmt.Errorf("trigger argocd refresh: %w", err)
		}

		return nil
	})
}

// WaitForApplicationReady waits for the ArgoCD application to be synced and healthy.
func (r *Reconciler) WaitForApplicationReady(ctx context.Context, timeout time.Duration) error {
	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(reconcilerPollInterval)
	defer ticker.Stop()

	appClient := r.applicationClient()

	for {
		select {
		case <-timeoutCtx.Done():
			return ErrReconcileTimeout
		case <-ticker.C:
			ready, err := r.checkApplicationStatus(timeoutCtx, appClient)
			if err != nil {
				return err
			}

			if ready {
				return nil
			}
		}
	}
}

// applicationClient returns a dynamic client for ArgoCD Applications.
func (r *Reconciler) applicationClient() dynamic.ResourceInterface {
	gvr := schema.GroupVersionResource{
		Group:    "argoproj.io",
		Version:  "v1alpha1",
		Resource: "applications",
	}

	return r.Dynamic.Resource(gvr).Namespace(DefaultNamespace)
}

// checkApplicationStatus checks if the application is synced and healthy.
func (r *Reconciler) checkApplicationStatus(
	ctx context.Context,
	client dynamic.ResourceInterface,
) (bool, error) {
	app, err := client.Get(ctx, rootApplicationName, metav1.GetOptions{})
	if err != nil {
		return false, fmt.Errorf("get argocd application: %w", err)
	}

	// Check for operation state first (ongoing sync operations)
	err = r.checkOperationState(app)
	if err != nil {
		return false, err
	}

	// Check for error conditions
	err = r.checkConditions(app)
	if err != nil {
		return false, err
	}

	// Check if synced and healthy
	return isApplicationSynced(app), nil
}

// checkOperationState checks if there's an operation in progress or failed.
func (r *Reconciler) checkOperationState(app *unstructured.Unstructured) error {
	operationState, found, _ := unstructured.NestedMap(app.Object, "status", "operationState")
	if !found {
		return nil // No operation in progress
	}

	phase, _, _ := unstructured.NestedString(operationState, "phase")
	message, _, _ := unstructured.NestedString(operationState, "message")

	if phase == "Error" || phase == "Failed" {
		// Check if this is a source-related error
		if isSourceRelatedError(message) {
			return fmt.Errorf("%w: %s", ErrSourceNotAvailable, message)
		}

		return fmt.Errorf("%w: %s", ErrOperationFailed, message)
	}

	return nil
}

// checkConditions checks for error conditions.
func (r *Reconciler) checkConditions(app *unstructured.Unstructured) error {
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
		condMessage, _, _ := unstructured.NestedString(condMap, "message")

		// Look for error conditions
		if condType == "ComparisonError" || condType == "SyncError" {
			if isSourceRelatedError(condMessage) {
				return fmt.Errorf("%w: %s", ErrSourceNotAvailable, condMessage)
			}
		}
	}

	return nil
}

// isSourceRelatedError checks if the error message indicates a source availability issue.
func isSourceRelatedError(message string) bool {
	sourceProblemPatterns := []string{
		"manifest unknown",
		"not found",
		"does not exist",
		"failed to fetch",
		"repository not found",
		"unable to resolve",
		"connection refused",
	}

	lowerMessage := strings.ToLower(message)
	for _, pattern := range sourceProblemPatterns {
		if strings.Contains(lowerMessage, pattern) {
			return true
		}
	}

	return false
}

// isApplicationSynced checks if the application is synced and healthy.
func isApplicationSynced(app *unstructured.Unstructured) bool {
	// Check sync status
	syncStatus, found, _ := unstructured.NestedString(app.Object, "status", "sync", "status")
	if !found || syncStatus != "Synced" {
		return false
	}

	// Check health status
	healthStatus, found, _ := unstructured.NestedString(app.Object, "status", "health", "status")
	if !found || healthStatus != "Healthy" {
		return false
	}

	return true
}
