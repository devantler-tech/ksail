package argocd

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/client/reconciler"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
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
	DefaultNamespace    = "argocd"
	rootApplicationName = "ksail"
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

// ReconcileOptions configures the reconciliation behavior.
type ReconcileOptions struct {
	// Timeout for waiting for application sync.
	Timeout time.Duration
	// HardRefresh requests ArgoCD to refresh caches.
	HardRefresh bool
}

// TriggerRefresh triggers an ArgoCD application refresh.
//
// A JSON merge patch is used instead of the traditional Get+Update approach.
// Patches are applied atomically server-side, so they never produce 409 Conflict
// errors even when ArgoCD controllers are concurrently updating the Application,
// which removes the need for an optimistic-concurrency retry loop.
func (r *Reconciler) TriggerRefresh(ctx context.Context, hardRefresh bool) error {
	refreshValue := "normal"
	if hardRefresh {
		refreshValue = argoCDHardRefreshAnnotation
	}

	patch := fmt.Appendf(nil,
		`{"metadata":{"annotations":{%q:%q}}}`,
		argoCDRefreshAnnotationKey,
		refreshValue,
	)

	_, err := r.applicationClient().Patch(
		ctx,
		rootApplicationName,
		types.MergePatchType,
		patch,
		metav1.PatchOptions{},
	)
	if err != nil {
		return fmt.Errorf("failed to trigger argocd refresh: %w", err)
	}

	return nil
}

// ApplicationInfo holds the name of an ArgoCD Application CR.
type ApplicationInfo struct {
	Name string
}

// ListApplications lists all ArgoCD Application CRs in the argocd namespace.
func (r *Reconciler) ListApplications(
	ctx context.Context,
) ([]ApplicationInfo, error) {
	client := r.applicationClient()

	list, err := client.List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list argocd applications: %w", err)
	}

	infos := make([]ApplicationInfo, 0, len(list.Items))

	for i := range list.Items {
		infos = append(infos, ApplicationInfo{Name: list.Items[i].GetName()})
	}

	return infos, nil
}

// CheckNamedApplicationReady performs a single-poll readiness check for
// a specific ArgoCD Application CR identified by name.
// Returns (ready, error) where error is non-nil for permanent failures.
func (r *Reconciler) CheckNamedApplicationReady(
	ctx context.Context,
	name string,
) (bool, error) {
	client := r.applicationClient()

	app, err := client.Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return false, fmt.Errorf("get argocd application %q: %w", name, err)
	}

	err = r.checkOperationState(app)
	if err != nil {
		return false, err
	}

	err = r.checkConditions(app)
	if err != nil {
		return false, err
	}

	return isApplicationSynced(app), nil
}

// applicationClient returns a dynamic client for ArgoCD Applications.
func (r *Reconciler) applicationClient() dynamic.ResourceInterface {
	return r.Dynamic.Resource(ApplicationGVR()).Namespace(DefaultNamespace)
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
	for _, cond := range reconciler.ParseConditions(app) {
		// Look for error conditions
		if cond.Type == "ComparisonError" || cond.Type == "SyncError" {
			if isSourceRelatedError(cond.Message) {
				return fmt.Errorf("%w: %s", ErrSourceNotAvailable, cond.Message)
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
