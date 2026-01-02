package workload

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	v1alpha1 "github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/cli/helpers"
	"github.com/devantler-tech/ksail/v5/pkg/client/argocd"
	runtime "github.com/devantler-tech/ksail/v5/pkg/di"
	iopath "github.com/devantler-tech/ksail/v5/pkg/io"
	"github.com/devantler-tech/ksail/v5/pkg/k8s"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/registry"
	"github.com/devantler-tech/ksail/v5/pkg/utils/notify"
	"github.com/devantler-tech/ksail/v5/pkg/utils/timer"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

//nolint:staticcheck // "GitOps" is a proper noun and must be capitalized
var errGitOpsEngineRequired = errors.New(
	"A GitOps engine must be enabled to reconcile workloads; " +
		"enable it with '--gitops-engine Flux|ArgoCD' during cluster init or " +
		"set 'spec.gitOpsEngine: Flux|ArgoCD' in ksail.yaml",
)

var (
	errFluxReconcileTimeout = errors.New(
		"timeout waiting for flux kustomization reconciliation",
	)
	errArgoCDReconcileTimeout    = errors.New("timeout waiting for argocd application sync")
	errFluxOCIRepositoryNotReady = errors.New(
		"flux OCIRepository is not ready - ensure you have pushed an artifact with 'ksail workload push'",
	)
	errArgoCDSourceNotAvailable = errors.New(
		"argocd application source is not available - ensure you have pushed an artifact with 'ksail workload push'",
	)
)

const (
	fluxNamespace                = "flux-system"
	fluxRootKustomizationName    = "flux-system"
	fluxRootOCIRepositoryName    = "flux-system"
	argoCDNamespace              = "argocd"
	argoCDRootApplicationName    = "ksail"
	defaultReconcileTimeout      = 5 * time.Minute
	reconcilePollInterval        = 2 * time.Second
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

	// Wait for OCIRepository to be ready - it needs time to fetch the artifact after push
	writeActivityNotification(
		"waiting for flux oci repository to be ready",
		outputTimer,
		writer,
	)

	ociRepoClient, err := getFluxOCIRepositoryClient(kubeconfigPath)
	if err != nil {
		return err
	}

	err = waitForFluxOCIRepositoryReady(ctx, ociRepoClient)
	if err != nil {
		return err
	}

	writeActivityNotification(
		"triggering flux kustomization reconciliation",
		outputTimer,
		writer,
	)

	kustomizationClient, err := getFluxKustomizationClient(kubeconfigPath)
	if err != nil {
		return err
	}

	err = triggerFluxReconciliation(ctx, kustomizationClient)
	if err != nil {
		return err
	}

	writeActivityNotification(
		"waiting for flux kustomization to reconcile",
		outputTimer,
		writer,
	)

	return waitForFluxReconciliation(ctx, kustomizationClient, timeout)
}

// getKubeconfigPath returns the kubeconfig path from config or default.
func getKubeconfigPath(clusterCfg *v1alpha1.Cluster) (string, error) {
	kubeconfigPath := strings.TrimSpace(clusterCfg.Spec.Cluster.Connection.Kubeconfig)
	if kubeconfigPath == "" {
		kubeconfigPath = v1alpha1.DefaultKubeconfigPath
	}

	expanded, err := iopath.ExpandHomePath(kubeconfigPath)
	if err != nil {
		return "", fmt.Errorf("expand kubeconfig path: %w", err)
	}

	return expanded, nil
}

// getFluxOCIRepositoryClient creates a dynamic client for Flux OCIRepositories.
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

	conditions, found, err := unstructured.NestedSlice(ociRepo.Object, "status", "conditions")
	if err != nil || !found || len(conditions) == 0 {
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

	applicationClient, err := getArgoCDApplicationClient(kubeconfigPath)
	if err != nil {
		return err
	}

	// Check Application status first to fail early if source is not available
	writeActivityNotification(
		"checking argocd application status",
		outputTimer,
		writer,
	)

	err = checkArgoCDApplicationSourceAvailable(ctx, applicationClient)
	if err != nil {
		return err
	}

	writeActivityNotification(
		"triggering argocd application refresh",
		outputTimer,
		writer,
	)

	err = triggerArgoCDRefresh(ctx, kubeconfigPath, artifactVersion)
	if err != nil {
		return err
	}

	writeActivityNotification(
		"waiting for argocd application to sync",
		outputTimer,
		writer,
	)

	return waitForArgoCDSync(ctx, applicationClient, timeout)
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
func getArgoCDApplicationClient(kubeconfigPath string) (dynamic.ResourceInterface, error) {
	applicationGVR := schema.GroupVersionResource{
		Group:    "argoproj.io",
		Version:  "v1alpha1",
		Resource: "applications",
	}

	return getDynamicResourceClient(kubeconfigPath, applicationGVR, argoCDNamespace)
}

// getDynamicResourceClient creates a dynamic Kubernetes client for a specific resource.
func getDynamicResourceClient(
	kubeconfigPath string,
	gvr schema.GroupVersionResource,
	namespace string,
) (dynamic.ResourceInterface, error) {
	restConfig, err := k8s.BuildRESTConfig(kubeconfigPath, "")
	if err != nil {
		return nil, fmt.Errorf("build rest config: %w", err)
	}

	dynamicClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("create dynamic client: %w", err)
	}

	return dynamicClient.Resource(gvr).Namespace(namespace), nil
}

// checkArgoCDApplicationSourceAvailable checks if the ArgoCD Application source is available.
// Returns an error immediately if the Application shows a source fetch failure.
func checkArgoCDApplicationSourceAvailable(
	ctx context.Context,
	applicationClient dynamic.ResourceInterface,
) error {
	app, err := applicationClient.Get(ctx, argoCDRootApplicationName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get argocd application: %w", err)
	}

	// Check operationState for source fetch errors
	err = checkArgoCDOperationState(app)
	if err != nil {
		return err
	}

	// Check conditions for repository errors
	return checkArgoCDConditions(app)
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

// waitForArgoCDSync waits for the application to sync and become healthy.
func waitForArgoCDSync(
	ctx context.Context,
	applicationClient dynamic.ResourceInterface,
	timeout time.Duration,
) error {
	return waitForResourceCondition(
		ctx,
		applicationClient,
		argoCDRootApplicationName,
		timeout,
		isArgoCDApplicationSynced,
		errArgoCDReconcileTimeout,
		"get argocd application status",
	)
}

// waitForResourceCondition is a generic function to wait for a resource to meet a condition.
func waitForResourceCondition(
	ctx context.Context,
	client dynamic.ResourceInterface,
	resourceName string,
	timeout time.Duration,
	checkCondition func(*unstructured.Unstructured) bool,
	timeoutErr error,
	errorMsg string,
) error {
	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(reconcilePollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-timeoutCtx.Done():
			return timeoutErr
		case <-ticker.C:
			resource, err := client.Get(timeoutCtx, resourceName, metav1.GetOptions{})
			if err != nil {
				return fmt.Errorf("%s: %w", errorMsg, err)
			}

			if checkCondition(resource) {
				return nil
			}
		}
	}
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

// NewReconcileCmd creates the workload reconcile command.
func NewReconcileCmd(_ *runtime.Runtime) *cobra.Command {
	cmd := &cobra.Command{
		Use:          "reconcile",
		Short:        "Trigger reconciliation for GitOps workloads",
		Long:         "Trigger reconciliation/sync for the root Flux kustomization or root ArgoCD application.",
		SilenceUsage: true,
	}

	cmd.Flags().Duration(
		"timeout",
		0,
		"timeout for waiting for reconciliation to complete (overrides config timeout)",
	)

	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		return runReconcile(cmd)
	}

	return cmd
}

// runReconcile executes the reconcile command logic.
func runReconcile(cmd *cobra.Command) error {
	ctx, err := initCommandContext(cmd)
	if err != nil {
		return err
	}

	clusterCfg := ctx.ClusterCfg
	outputTimer := ctx.OutputTimer
	tmr := ctx.Timer

	// Determine GitOps engine - use config if set, otherwise auto-detect
	gitOpsEngine := clusterCfg.Spec.Cluster.GitOpsEngine
	if gitOpsEngine == v1alpha1.GitOpsEngineNone || gitOpsEngine == "" {
		detected, detectErr := autoDetectGitOpsEngine(cmd, tmr, outputTimer)
		if detectErr != nil {
			return detectErr
		}

		gitOpsEngine = detected
	}

	timeout, err := getReconcileTimeout(cmd, clusterCfg)
	if err != nil {
		return err
	}

	cmd.Println()
	notify.WriteMessage(notify.Message{
		Type:    notify.TitleType,
		Emoji:   "🔄",
		Content: "Trigger Reconciliation...",
		Writer:  cmd.OutOrStdout(),
	})

	tmr.NewStage()

	err = executeReconciliation(cmd, clusterCfg, gitOpsEngine, timeout, outputTimer)
	if err != nil {
		return err
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.SuccessType,
		Content: "reconciliation completed",
		Timer:   outputTimer,
		Writer:  cmd.OutOrStdout(),
	})

	return nil
}

// autoDetectGitOpsEngine detects the GitOps engine from the cluster.
func autoDetectGitOpsEngine(
	cmd *cobra.Command,
	tmr timer.Timer,
	outputTimer timer.Timer,
) (v1alpha1.GitOpsEngine, error) {
	cmd.Println()
	notify.WriteMessage(notify.Message{
		Type:    notify.TitleType,
		Emoji:   "🔎",
		Content: "Auto-detect GitOps engine...",
		Writer:  cmd.OutOrStdout(),
	})

	tmr.NewStage()

	notify.WriteMessage(notify.Message{
		Type:    notify.ActivityType,
		Content: "detecting gitops engine in cluster",
		Timer:   outputTimer,
		Writer:  cmd.OutOrStdout(),
	})

	engine, err := helpers.DetectGitOpsEngine(cmd.Context())
	if err != nil {
		return v1alpha1.GitOpsEngineNone, fmt.Errorf("detect gitops engine: %w", err)
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.SuccessType,
		Content: "%s detected",
		Args:    []any{engine},
		Timer:   outputTimer,
		Writer:  cmd.OutOrStdout(),
	})

	return engine, nil
}

// getReconcileTimeout determines the timeout from flag, config, or default.
func getReconcileTimeout(cmd *cobra.Command, clusterCfg *v1alpha1.Cluster) (time.Duration, error) {
	timeout, err := cmd.Flags().GetDuration("timeout")
	if err != nil {
		return 0, fmt.Errorf("get timeout flag: %w", err)
	}

	if timeout == 0 {
		if clusterCfg.Spec.Cluster.Connection.Timeout.Duration > 0 {
			timeout = clusterCfg.Spec.Cluster.Connection.Timeout.Duration
		} else {
			timeout = defaultReconcileTimeout
		}
	}

	return timeout, nil
}

// executeReconciliation runs the appropriate reconciliation based on GitOps engine.
func executeReconciliation(
	cmd *cobra.Command,
	clusterCfg *v1alpha1.Cluster,
	gitOpsEngine v1alpha1.GitOpsEngine,
	timeout time.Duration,
	outputTimer timer.Timer,
) error {
	artifactVersion := registry.DefaultLocalArtifactTag

	switch gitOpsEngine {
	case v1alpha1.GitOpsEngineArgoCD:
		return reconcileArgoCDApplication(
			cmd.Context(),
			clusterCfg,
			artifactVersion,
			timeout,
			outputTimer,
			cmd.OutOrStdout(),
		)
	case v1alpha1.GitOpsEngineFlux:
		return reconcileFluxKustomization(
			cmd.Context(),
			clusterCfg,
			timeout,
			outputTimer,
			cmd.OutOrStdout(),
		)
	case v1alpha1.GitOpsEngineNone:
		return errGitOpsEngineRequired
	default:
		return errGitOpsEngineRequired
	}
}
