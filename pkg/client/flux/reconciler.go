package flux

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/client/reconciler"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

// Reconciler errors.
var (
	// ErrReconcileTimeout is returned when reconciliation times out.
	ErrReconcileTimeout = errors.New(
		"timeout waiting for flux kustomization reconciliation - " +
			"verify cluster health, Flux controllers status, and network/connectivity to the cluster",
	)
	// ErrOCIRepositoryNotReady is returned when the OCIRepository is not ready.
	ErrOCIRepositoryNotReady = errors.New(
		"flux OCIRepository is not ready - ensure you have pushed an artifact with 'ksail workload push'",
	)
	// ErrKustomizationFailed is returned when the Kustomization reconciliation fails.
	ErrKustomizationFailed = errors.New(
		"flux kustomization reconciliation failed - check the Kustomization status and Flux controller logs for details",
	)
)

// Substrings used to detect specific error conditions from error messages.
const (
	ociErrManifestUnknownSubstr     = "manifest unknown"
	ociErrDoesNotExistSubstr        = "does not exist"
	apiDiscoveryNotFoundSubstr      = "the server could not find the requested resource"
	apiDiscoveryNoMatchesKindSubstr = "no matches for kind"
)

// Reconciler constants.
const (
	rootKustomizationName     = "flux-system"
	rootOCIRepositoryName     = "flux-system"
	ociRepositoryReadyTimeout = 2 * time.Minute
	pollInterval              = 500 * time.Millisecond
	reconcileAnnotationKey    = "reconcile.fluxcd.io/requestedAt"

	// Condition type and status constants.
	conditionTypeReady   = "Ready"
	conditionTypeStalled = "Stalled"
	conditionStatusTrue  = "True"
	conditionStatusFalse = "False"

	// API availability timeout for reconciliation operations - should be long enough
	// for the Flux controllers to become ready in slow CI environments.
	apiAvailabilityTimeout      = 2 * time.Minute
	apiAvailabilityPollInterval = 500 * time.Millisecond
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

// ReconcileOptions configures the reconciliation behavior.
type ReconcileOptions struct {
	// Timeout for waiting for OCIRepository readiness and Kustomization reconciliation.
	Timeout time.Duration
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
// If timeout is zero or negative, the default ociRepositoryReadyTimeout is used.
func (r *Reconciler) WaitForOCIRepositoryReady(ctx context.Context, timeout time.Duration) error {
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
		ready, err := r.pollOCIRepositoryStatus(timeoutCtx, ociRepoClient, &lastErr)
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

		infos = append(infos, KustomizationInfo{Name: name, Path: path, DependsOn: dependsOn})
	}

	return infos, nil
}

// ListKustomizationPaths lists all Flux Kustomization CRs in the default
// namespace and returns their names and spec.path values.
func (r *Reconciler) ListKustomizationPaths(
	ctx context.Context,
) ([]KustomizationInfo, error) {
	return r.ListKustomizations(ctx)
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

	return checkKustomizationStatus(kustomization)
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

// pollOCIRepositoryStatus checks OCI repository status with timeout guard.
// It returns (ready, nil) on success, (false, nil) for transient errors (stored in lastErr),
// or (false, err) for permanent/timeout errors.
func (r *Reconciler) pollOCIRepositoryStatus(
	ctx context.Context,
	client dynamic.ResourceInterface,
	lastErr *error,
) (bool, error) {
	err := ctx.Err()
	if err != nil {
		return false, ociTimeoutError(*lastErr)
	}

	ready, err := r.checkOCIRepositoryStatus(ctx, client)
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

// isAPIDiscoveryError checks if the error indicates the API discovery is incomplete.
func isAPIDiscoveryError(errMsg string) bool {
	// "the server could not find the requested resource" indicates the CRD endpoint
	// isn't fully registered yet or the Flux controllers haven't started
	if strings.Contains(errMsg, apiDiscoveryNotFoundSubstr) {
		return true
	}

	// "no matches for kind" is a REST mapper error when the CRD isn't known yet
	return strings.Contains(errMsg, apiDiscoveryNoMatchesKindSubstr)
}

// isConnectionError checks if the error is a network connection error.
func isConnectionError(errMsg string) bool {
	return strings.Contains(errMsg, "connection refused") ||
		strings.Contains(errMsg, "connection reset") ||
		strings.Contains(errMsg, "i/o timeout") ||
		strings.Contains(errMsg, "EOF")
}

// isTransientAPIError checks if the error is a transient API error that should be retried.
// This includes errors that occur when the Flux CRDs or controllers aren't fully ready yet,
// which can happen in slow CI environments or shortly after cluster creation.
func isTransientAPIError(err error) bool {
	if err == nil {
		return false
	}

	// Check for specific status errors that indicate the API isn't ready
	if apierrors.IsServiceUnavailable(err) ||
		apierrors.IsTimeout(err) ||
		apierrors.IsTooManyRequests(err) ||
		apierrors.IsConflict(err) {
		return true
	}

	// NotFound is transient because the resource may not exist yet.
	// The Instance controller creates OCIRepository and Kustomization resources
	// asynchronously, so they might not exist immediately after Instance creation.
	// The retry loop has a timeout, so if the resource truly doesn't exist, it will fail.
	if apierrors.IsNotFound(err) {
		return true
	}

	errMsg := err.Error()

	// Check for API discovery errors
	if isAPIDiscoveryError(errMsg) {
		return true
	}

	// Check for connection errors
	return isConnectionError(errMsg)
}

// handleTransientError waits for the next retry or returns a timeout error.
func handleTransientError(
	waitCtx context.Context,
	ticker *time.Ticker,
	resourceDescription string,
	err error,
) error {
	select {
	case <-waitCtx.Done():
		return fmt.Errorf(
			"timed out waiting for %s to be available: %w",
			resourceDescription,
			err,
		)
	case <-ticker.C:
		return nil // Continue retry loop
	}
}

// triggerReconciliationWithRetry triggers reconciliation on a Flux resource with retry logic
// for handling optimistic concurrency conflicts and transient API errors.
// This is necessary because Flux controllers may not be fully ready immediately after
// cluster creation, especially in slow CI environments.
func triggerReconciliationWithRetry(
	ctx context.Context,
	client dynamic.ResourceInterface,
	resourceName string,
	resourceDescription string,
) error {
	// Create a timeout context for the entire retry operation
	waitCtx, cancel := context.WithTimeout(ctx, apiAvailabilityTimeout)
	defer cancel()

	ticker := time.NewTicker(apiAvailabilityPollInterval)
	defer ticker.Stop()

	var lastErr error

	for {
		resource, err := client.Get(waitCtx, resourceName, metav1.GetOptions{})
		if err != nil {
			// Check if this is a transient API error that should be retried
			if isTransientAPIError(err) {
				lastErr = err

				retryErr := handleTransientError(waitCtx, ticker, resourceDescription, lastErr)
				if retryErr != nil {
					return retryErr
				}

				continue
			}

			// Non-transient error, fail immediately
			return fmt.Errorf("get %s: %w", resourceDescription, err)
		}

		// Resource found, try to update it
		annotations := resource.GetAnnotations()
		if annotations == nil {
			annotations = make(map[string]string)
		}

		annotations[reconcileAnnotationKey] = time.Now().Format(time.RFC3339Nano)
		resource.SetAnnotations(annotations)

		_, err = client.Update(waitCtx, resource, metav1.UpdateOptions{})
		if err == nil {
			return nil // Success
		}

		// Check if this is a transient error (conflict or API not ready)
		if isTransientAPIError(err) {
			lastErr = err

			select {
			case <-waitCtx.Done():
				return fmt.Errorf(
					"timed out updating %s: %w",
					resourceDescription,
					lastErr,
				)
			case <-ticker.C:
				continue
			}
		}

		// Non-transient error on update, fail immediately
		return fmt.Errorf("trigger %s reconciliation: %w", resourceDescription, err)
	}
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
