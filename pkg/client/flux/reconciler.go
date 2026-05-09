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
	"k8s.io/apimachinery/pkg/types"
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

// ReconcileExcludeAnnotation is the KSail annotation key that, when set to
// "true" on a Flux Kustomization CR, excludes that kustomization from
// KSail's progress monitoring and readiness polling during
// `ksail workload reconcile`. Flux still reconciles the resource; only
// KSail's waiting phase is skipped.
const ReconcileExcludeAnnotation = "ksail.devantler.tech/reconcile-exclude"

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

	// HelmRelease reset constants.
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
// It uses a JSON merge patch with retry logic for transient API errors (e.g. resource not
// yet created, API server temporarily unavailable).
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

// helmReleaseGVR returns the GroupVersionResource for Flux HelmReleases.
func helmReleaseGVR() schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group:    "helm.toolkit.fluxcd.io",
		Version:  "v2",
		Resource: "helmreleases",
	}
}

// ListStuckHelmReleases returns all HelmReleases across all namespaces that are
// stuck in a non-recoverable state. A HelmRelease is considered stuck when its
// Ready condition is False with a failure reason (e.g., InstallFailed,
// UpgradeFailed) or when the Stalled condition is True.
// Returns an empty list without error when the HelmRelease CRD is not installed.
func (r *Reconciler) ListStuckHelmReleases(
	ctx context.Context,
) ([]StuckHelmRelease, error) {
	client := r.Dynamic.Resource(helmReleaseGVR())

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

	gvr := helmReleaseGVR()

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

	conditions, found, _ := unstructured.NestedSlice(helmRelease.Object, "status", "conditions")
	if !found || len(conditions) == 0 {
		return nil
	}

	return evaluateHelmReleaseConditions(helmRelease, conditions)
}

// evaluateHelmReleaseConditions scans a HelmRelease's conditions and returns
// a StuckHelmRelease for the first stuck condition found, or nil if healthy.
func evaluateHelmReleaseConditions(
	helmRelease *unstructured.Unstructured,
	conditions []any,
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

// conditionDetails holds the parsed fields from a Flux status condition map.
type conditionDetails struct {
	condType    string
	condStatus  string
	condReason  string
	condMessage string
}

// parseCondition extracts the standard condition fields from a raw condition
// map entry. Returns nil when the entry is not a map.
func parseCondition(cond any) *conditionDetails {
	condMap, ok := cond.(map[string]any)
	if !ok {
		return nil
	}

	condType, _, _ := unstructured.NestedString(condMap, "type")
	condStatus, _, _ := unstructured.NestedString(condMap, "status")
	condReason, _, _ := unstructured.NestedString(condMap, "reason")
	condMessage, _, _ := unstructured.NestedString(condMap, "message")

	return &conditionDetails{
		condType:    condType,
		condStatus:  condStatus,
		condReason:  condReason,
		condMessage: condMessage,
	}
}

// evaluateHelmReleaseCondition checks a single condition map and returns a
// StuckHelmRelease if the condition indicates a stuck state, or nil otherwise.
func evaluateHelmReleaseCondition(
	helmRelease *unstructured.Unstructured,
	cond any,
	stuckReasons []string,
) *StuckHelmRelease {
	condDetails := parseCondition(cond)
	if condDetails == nil {
		return nil
	}

	// Stalled=True means the controller has given up retrying.
	if condDetails.condType == conditionTypeStalled &&
		condDetails.condStatus == conditionStatusTrue {
		return &StuckHelmRelease{
			Name:      helmRelease.GetName(),
			Namespace: helmRelease.GetNamespace(),
			Reason:    condDetails.condReason,
			Message:   condDetails.condMessage,
		}
	}

	// Ready=False with a failure reason.
	if condDetails.condType == conditionTypeReady &&
		condDetails.condStatus == conditionStatusFalse &&
		slices.Contains(stuckReasons, condDetails.condReason) {
		return &StuckHelmRelease{
			Name:      helmRelease.GetName(),
			Namespace: helmRelease.GetNamespace(),
			Reason:    condDetails.condReason,
			Message:   condDetails.condMessage,
		}
	}

	return nil
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
// for handling transient API errors (e.g. resource not yet created in slow CI environments).
//
// A JSON merge patch is used instead of the traditional Get+Update approach.  Patches
// are applied atomically server-side, so they never produce 409 Conflict errors even
// when Flux controllers are concurrently updating the same resource status.  This
// prevents the retry loop from running for the full apiAvailabilityTimeout when the
// cluster has many HelmReleases or kustomizations.
func triggerReconciliationWithRetry(
	ctx context.Context,
	client dynamic.ResourceInterface,
	resourceName string,
	resourceDescription string,
) error {
	// Create a timeout context for the entire retry operation.
	waitCtx, cancel := context.WithTimeout(ctx, apiAvailabilityTimeout)
	defer cancel()

	ticker := time.NewTicker(apiAvailabilityPollInterval)
	defer ticker.Stop()

	// Build the merge patch once; the timestamp is set at trigger time.
	patch := []byte(fmt.Sprintf(
		`{"metadata":{"annotations":{%q:%q}}}`,
		reconcileAnnotationKey,
		time.Now().Format(time.RFC3339Nano),
	))

	var lastErr error

	for {
		// Guard against an expired context before making an API call.
		// Without this check, an expired context causes the k8s rate limiter to
		// return "rate: Wait(n=1) would exceed context deadline", which is not
		// recognised as a context error and propagates as a confusing permanent
		// failure.
		if err := waitCtx.Err(); err != nil {
			if lastErr != nil {
				return fmt.Errorf(
					"timed out waiting for %s to be available: %w",
					resourceDescription,
					lastErr,
				)
			}

			return fmt.Errorf("trigger %s reconciliation: %w", resourceDescription, err)
		}

		_, err := client.Patch(waitCtx, resourceName, types.MergePatchType, patch, metav1.PatchOptions{})
		if err == nil {
			return nil
		}

		if isTransientAPIError(err) {
			lastErr = err

			retryErr := handleTransientError(waitCtx, ticker, resourceDescription, lastErr)
			if retryErr != nil {
				return retryErr
			}

			continue
		}

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
		condDetails := parseCondition(condition)
		if condDetails == nil {
			continue
		}

		if condDetails.condType == conditionTypeReady {
			readyStatus = condDetails.condStatus
			readyReason = condDetails.condReason
			readyMessage = condDetails.condMessage

			if condDetails.condStatus == conditionStatusTrue {
				return true, conditionTypeReady, nil
			}
		}

		// Check for Stalled condition which indicates a permanent failure.
		if condDetails.condType == conditionTypeStalled &&
			condDetails.condStatus == conditionStatusTrue {
			return false, "", fmt.Errorf(
				"%w: stalled - %s",
				ErrKustomizationFailed,
				condDetails.condMessage,
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
