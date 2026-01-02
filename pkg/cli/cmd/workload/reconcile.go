package workload

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	v1alpha1 "github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/cli/helpers"
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

// Shared errors.
//
//nolint:staticcheck // "GitOps" is a proper noun and must be capitalized
var errGitOpsEngineRequired = errors.New(
	"A GitOps engine must be enabled to reconcile workloads; " +
		"enable it with '--gitops-engine Flux|ArgoCD' during cluster init or " +
		"set 'spec.gitOpsEngine: Flux|ArgoCD' in ksail.yaml",
)

// Shared constants for reconciliation.
const (
	defaultReconcileTimeout = 5 * time.Minute
	reconcilePollInterval   = 2 * time.Second
)

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
