package argocd

import (
	"context"
	"errors"
	"fmt"
	"regexp"
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
// Active operation and comparison failures return an error even when sync and
// health fields still describe a successful comparison. Callers may use
// IsColdStartTransient to retry recognized transport failures within a deadline.
func (r *Reconciler) CheckNamedApplicationReady(
	ctx context.Context,
	name string,
) (bool, error) {
	client := r.applicationClient()

	app, err := client.Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return false, fmt.Errorf("get argocd application %q: %w", name, err)
	}

	err = preferPermanentFailure(r.checkOperationState(app), r.checkConditions(app))
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
		return classifyApplicationError(message, false)
	}

	return nil
}

// checkConditions checks for error conditions.
func (r *Reconciler) checkConditions(app *unstructured.Unstructured) error {
	var failure error

	for _, cond := range reconciler.ParseConditions(app) {
		if cond.Type == "ComparisonError" || cond.Type == "SyncError" {
			failure = preferPermanentFailure(
				failure,
				classifyApplicationError(cond.Message, cond.Type == "ComparisonError"),
			)
		}
	}

	return failure
}

// sourceAvailabilityError preserves the public sentinel while recording whether
// the source failed for a recognized transport reason.
type sourceAvailabilityError struct {
	message   string
	transient bool
}

var errPermanentApplicationFailure = errors.New("permanent ArgoCD application failure")

// comparisonHTTPStatus requires an HTTP/status label so line numbers and ports
// do not turn invalid manifests into transient transport failures.
var comparisonHTTPStatus = regexp.MustCompile(
	`\b(?:http(?:/[0-9.]+)?|status(?:\s+code)?)[\s:=]+(?:429|5[0-9]{2})\b`,
)

// Error includes the source failure and the public sentinel's existing guidance.
func (e *sourceAvailabilityError) Error() string {
	return fmt.Sprintf("%s: %s", ErrSourceNotAvailable, e.message)
}

// Unwrap preserves errors.Is compatibility for callers checking source availability.
func (e *sourceAvailabilityError) Unwrap() error {
	return ErrSourceNotAvailable
}

// Is exposes permanent classification through wrappers and errors.Join without
// changing the diagnostic text or the existing source-availability sentinel.
func (e *sourceAvailabilityError) Is(target error) bool {
	return target == errPermanentApplicationFailure && !e.transient
}

// IsPermanentApplicationError reports whether any wrapped or joined application
// error is permanent. Outer retries must honor this before matching network text.
func IsPermanentApplicationError(err error) bool {
	return errors.Is(err, errPermanentApplicationFailure) || errors.Is(err, ErrOperationFailed)
}

// classifyApplicationError retries only recognized transport failures and rejects ambiguous errors.
func classifyApplicationError(message string, comparison bool) error {
	lower := strings.ToLower(message)

	// Explicit absence and denied access stay terminal even when the message also
	// contains a transport failure from an earlier attempt.
	if containsAny(lower, "manifest unknown", "not found", "does not exist") {
		return &sourceAvailabilityError{message: message}
	}

	if containsAny(lower, "unauthorized", "unauthenticated", "authentication required",
		"permission denied", "permissiondenied", "access denied", "forbidden", "x509:") {
		return fmt.Errorf("%w: %s", ErrOperationFailed, message)
	}

	if containsAny(lower, "connection refused", "connection reset by peer", "i/o timeout",
		"no such host", "temporary failure in name resolution", "network is unreachable",
		"tls handshake timeout") {
		return &sourceAvailabilityError{message: message, transient: true}
	}

	if comparison && comparisonTransportError(lower) {
		return &sourceAvailabilityError{message: message, transient: true}
	}

	if containsAny(lower, "failed to fetch", "unable to resolve") {
		return &sourceAvailabilityError{message: message}
	}

	return fmt.Errorf("%w: %s", ErrOperationFailed, message)
}

// comparisonTransportError recognizes transport details whose meaning is
// ambiguous in sync hooks or failed operation states. EOF must end the detail.
func comparisonTransportError(message string) bool {
	lower := strings.TrimSpace(message)

	return containsAny(lower, "context deadline exceeded", "too many requests",
		"internal server error", "bad gateway", "service unavailable", "gateway timeout") ||
		comparisonHTTPStatus.MatchString(lower) || lower == "eof" ||
		strings.HasSuffix(lower, "unexpected eof") || strings.HasSuffix(lower, ": eof") ||
		strings.HasSuffix(lower, "= eof")
}

// containsAny matches normalized ArgoCD diagnostics against recognized failure descriptions.
func containsAny(message string, patterns ...string) bool {
	for _, pattern := range patterns {
		if strings.Contains(message, pattern) {
			return true
		}
	}

	return false
}

// preferPermanentFailure prevents a retryable error from hiding another failure on the same Application.
func preferPermanentFailure(current, candidate error) error {
	if candidate != nil && (current == nil || isTransientSourceError(current)) {
		return candidate
	}

	return current
}

// isTransientSourceError accepts only source failures classified as transport errors.
func isTransientSourceError(err error) bool {
	var sourceErr *sourceAvailabilityError

	return !IsPermanentApplicationError(err) && errors.As(err, &sourceErr) && sourceErr.transient
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

// IsColdStartTransient reports whether err is a recognized transport failure
// within the caller's cold-start grace period. ArgoCD retries these failures as
// its services start. Explicit source absence, access failures, and unclassified
// errors remain terminal; elapsed time never makes those failures retryable.
func IsColdStartTransient(err error, elapsed, grace time.Duration) bool {
	return isTransientSourceError(err) && elapsed < grace
}
