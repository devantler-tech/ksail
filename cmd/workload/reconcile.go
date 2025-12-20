package workload

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	v1alpha1 "github.com/devantler-tech/ksail/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/pkg/client/argocd"
	cmdhelpers "github.com/devantler-tech/ksail/pkg/cmd"
	runtime "github.com/devantler-tech/ksail/pkg/di"
	iopath "github.com/devantler-tech/ksail/pkg/io"
	ksailconfigmanager "github.com/devantler-tech/ksail/pkg/io/config-manager/ksail"
	"github.com/devantler-tech/ksail/pkg/svc/provisioner/registry"
	"github.com/devantler-tech/ksail/pkg/ui/notify"
	"github.com/devantler-tech/ksail/pkg/ui/timer"
	"github.com/spf13/cobra"
)

var errLocalRegistryRequired = errors.New(
	"local registry and a gitops engine must be enabled to reconcile workloads; " +
		"enable it with '--local-registry Enabled' and '--gitops-engine Flux|ArgoCD' " +
		"during cluster init or set 'spec.localRegistry: Enabled' and " +
		"'spec.gitOpsEngine: Flux' in ksail.yaml",
)

var errGitOpsEngineRequired = errors.New(
	"a gitops engine must be enabled to reconcile workloads; " +
		"enable it with '--gitops-engine Flux|ArgoCD' during cluster init or " +
		"set 'spec.gitOpsEngine: Flux|ArgoCD' in ksail.yaml",
)

// refreshArgoCDApplication refreshes the ArgoCD application with the new artifact version.
func refreshArgoCDApplication(
	ctx context.Context,
	clusterCfg *v1alpha1.Cluster,
	artifactVersion string,
	outputTimer timer.Timer,
	writer io.Writer,
) error {
	kubeconfigPath := strings.TrimSpace(clusterCfg.Spec.Connection.Kubeconfig)
	if kubeconfigPath == "" {
		kubeconfigPath = v1alpha1.DefaultKubeconfigPath
	}

	kubeconfigPath, err := iopath.ExpandHomePath(kubeconfigPath)
	if err != nil {
		return fmt.Errorf("expand kubeconfig path: %w", err)
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.ActivityType,
		Content: "refreshing argocd application",
		Timer:   outputTimer,
		Writer:  writer,
	})

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

// NewReconcileCmd creates the workload reconcile command.
func NewReconcileCmd(_ *runtime.Runtime) *cobra.Command {
	cmd := &cobra.Command{
		Use:          "reconcile",
		Short:        "Trigger reconciliation for GitOps workloads",
		Long:         "Trigger reconciliation/sync for the root Flux kustomization or root ArgoCD application.",
		SilenceUsage: true,
	}

	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		tmr := timer.New()
		tmr.Start()

		fieldSelectors := ksailconfigmanager.DefaultClusterFieldSelectors()
		cfgManager := ksailconfigmanager.NewCommandConfigManager(cmd, fieldSelectors)

		outputTimer := cmdhelpers.MaybeTimer(cmd, tmr)

		clusterCfg, err := cfgManager.LoadConfig(outputTimer)
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}

		gitOpsEngineConfigured := clusterCfg.Spec.GitOpsEngine != v1alpha1.GitOpsEngineNone

		if !gitOpsEngineConfigured {
			return errGitOpsEngineRequired
		}

		artifactVersion := registry.DefaultLocalArtifactTag

		cmd.Println()
		notify.WriteMessage(notify.Message{
			Type:    notify.TitleType,
			Emoji:   "🔄",
			Content: "Trigger Reconciliation...",
			Writer:  cmd.OutOrStdout(),
		})

		tmr.NewStage()

		if clusterCfg.Spec.GitOpsEngine == v1alpha1.GitOpsEngineArgoCD {
			err = refreshArgoCDApplication(
				cmd.Context(),
				clusterCfg,
				artifactVersion,
				outputTimer,
				cmd.OutOrStdout(),
			)
			if err != nil {
				return err
			}
		}

		notify.WriteMessage(notify.Message{
			Type:    notify.SuccessType,
			Content: "reconciliation triggered",
			Timer:   outputTimer,
			Writer:  cmd.OutOrStdout(),
		})

		return nil
	}

	return cmd
}
