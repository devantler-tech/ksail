package workload

import (
	"errors"
	"fmt"
	"strings"
	"time"

	v1alpha1 "github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v5/pkg/cli/helpers"
	"github.com/devantler-tech/ksail/v5/pkg/client/argocd"
	"github.com/devantler-tech/ksail/v5/pkg/client/flux"
	runtime "github.com/devantler-tech/ksail/v5/pkg/di"
	iopath "github.com/devantler-tech/ksail/v5/pkg/io"
	"github.com/devantler-tech/ksail/v5/pkg/notify"
	"github.com/devantler-tech/ksail/v5/pkg/timer"
	"github.com/spf13/cobra"
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
const defaultReconcileTimeout = 5 * time.Minute

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

// NewReconcileCmd creates the workload reconcile command.
func NewReconcileCmd(_ *runtime.Runtime) *cobra.Command {
	cmd := &cobra.Command{
		Use:          "reconcile",
		Short:        "Trigger reconciliation for GitOps workloads",
		Long:         "Trigger reconciliation/sync for the root Flux kustomization or root ArgoCD application.",
		SilenceUsage: true,
		Annotations: map[string]string{
			annotations.AnnotationPermission: "write",
		},
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
	kubeconfigPath, err := getKubeconfigPath(clusterCfg)
	if err != nil {
		return err
	}

	switch gitOpsEngine {
	case v1alpha1.GitOpsEngineArgoCD:
		return reconcileArgoCD(cmd, kubeconfigPath, timeout, outputTimer)
	case v1alpha1.GitOpsEngineFlux:
		return reconcileFlux(cmd, kubeconfigPath, timeout, outputTimer)
	case v1alpha1.GitOpsEngineNone:
		return errGitOpsEngineRequired
	default:
		return errGitOpsEngineRequired
	}
}

// reconcileFlux triggers and waits for Flux reconciliation using the client reconciler.
func reconcileFlux(
	cmd *cobra.Command,
	kubeconfigPath string,
	timeout time.Duration,
	outputTimer timer.Timer,
) error {
	reconciler, err := flux.NewReconciler(kubeconfigPath)
	if err != nil {
		return fmt.Errorf("create flux reconciler: %w", err)
	}

	writeActivityNotification(
		"triggering flux oci repository reconciliation",
		outputTimer,
		cmd.OutOrStdout(),
	)

	err = reconciler.TriggerOCIRepositoryReconciliation(cmd.Context())
	if err != nil {
		return fmt.Errorf("trigger oci repository reconciliation: %w", err)
	}

	writeActivityNotification(
		"waiting for flux oci repository to be ready",
		outputTimer,
		cmd.OutOrStdout(),
	)

	err = reconciler.WaitForOCIRepositoryReady(cmd.Context())
	if err != nil {
		return fmt.Errorf("wait for oci repository ready: %w", err)
	}

	writeActivityNotification(
		"triggering flux kustomization reconciliation",
		outputTimer,
		cmd.OutOrStdout(),
	)

	err = reconciler.TriggerKustomizationReconciliation(cmd.Context())
	if err != nil {
		return fmt.Errorf("trigger kustomization reconciliation: %w", err)
	}

	writeActivityNotification(
		"waiting for flux kustomization to reconcile",
		outputTimer,
		cmd.OutOrStdout(),
	)

	err = reconciler.WaitForKustomizationReady(cmd.Context(), timeout)
	if err != nil {
		return fmt.Errorf("wait for kustomization ready: %w", err)
	}

	return nil
}

// reconcileArgoCD triggers and waits for ArgoCD application sync using the client reconciler.
func reconcileArgoCD(
	cmd *cobra.Command,
	kubeconfigPath string,
	timeout time.Duration,
	outputTimer timer.Timer,
) error {
	reconciler, err := argocd.NewReconciler(kubeconfigPath)
	if err != nil {
		return fmt.Errorf("create argocd reconciler: %w", err)
	}

	writeActivityNotification(
		"triggering argocd application refresh",
		outputTimer,
		cmd.OutOrStdout(),
	)

	err = reconciler.TriggerRefresh(cmd.Context(), true) // Always hard refresh
	if err != nil {
		return fmt.Errorf("trigger argocd refresh: %w", err)
	}

	writeActivityNotification(
		"waiting for argocd application to sync",
		outputTimer,
		cmd.OutOrStdout(),
	)

	err = reconciler.WaitForApplicationReady(cmd.Context(), timeout)
	if err != nil {
		return fmt.Errorf("wait for argocd application ready: %w", err)
	}

	return nil
}
