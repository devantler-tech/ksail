package components

import (
	"context"
	"fmt"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/cli/create"
	"github.com/devantler-tech/ksail/v5/pkg/cli/flags"
	"github.com/devantler-tech/ksail/v5/pkg/cli/ui/notify"
	"github.com/spf13/cobra"
)

const (
	fluxResourcesActivity   = "applying custom resources"
	argoCDResourcesActivity = "configuring argocd resources"
)

// InstallPostCNIComponents installs all post-CNI components in parallel.
// This includes metrics-server, CSI, cert-manager, and GitOps engines (Flux/ArgoCD).
func InstallPostCNIComponents(
	cmd *cobra.Command,
	clusterCfg *v1alpha1.Cluster,
	factories *create.InstallerFactories,
	tmr interface{},
	firstActivityShown *bool,
) error {
	needsMetricsServer := create.NeedsMetricsServerInstall(clusterCfg)
	needsCSI := clusterCfg.Spec.Cluster.CSI == v1alpha1.CSILocalPathStorage
	needsCertManager := clusterCfg.Spec.Cluster.CertManager == v1alpha1.CertManagerEnabled
	needsArgoCD := clusterCfg.Spec.Cluster.GitOpsEngine == v1alpha1.GitOpsEngineArgoCD
	needsFlux := clusterCfg.Spec.Cluster.GitOpsEngine == v1alpha1.GitOpsEngineFlux

	componentCount := 0
	if needsMetricsServer {
		componentCount++
	}

	if needsCSI {
		componentCount++
	}

	if needsCertManager {
		componentCount++
	}

	if needsArgoCD {
		componentCount++
	}

	if needsFlux {
		componentCount++
	}

	if componentCount == 0 {
		return nil
	}

	if *firstActivityShown {
		_, _ = fmt.Fprintln(cmd.OutOrStdout())
	}

	*firstActivityShown = true

	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	var (
		gitOpsKubeconfig    string
		gitOpsKubeconfigErr error
	)

	if needsArgoCD || needsFlux {
		_, gitOpsKubeconfig, gitOpsKubeconfigErr = factories.HelmClientFactory(clusterCfg)
		if gitOpsKubeconfigErr != nil {
			return fmt.Errorf("failed to create helm client for gitops: %w", gitOpsKubeconfigErr)
		}
	}

	err := installComponentsInParallel(ctx, cmd, clusterCfg, factories, tmr, needsMetricsServer, needsCSI, needsCertManager, needsArgoCD, needsFlux)
	if err != nil {
		return err
	}

	return configureGitOpsResources(ctx, cmd, clusterCfg, factories, needsArgoCD, needsFlux, gitOpsKubeconfig)
}

func installComponentsInParallel(
	ctx context.Context,
	cmd *cobra.Command,
	clusterCfg *v1alpha1.Cluster,
	factories *create.InstallerFactories,
	tmr interface{},
	needsMetricsServer, needsCSI, needsCertManager, needsArgoCD, needsFlux bool,
) error {
	var tasks []notify.ProgressTask

	if needsMetricsServer {
		tasks = append(tasks, notify.ProgressTask{
			Name: "metrics-server",
			Fn: func(taskCtx context.Context) error {
				return create.InstallMetricsServerSilent(taskCtx, clusterCfg, factories)
			},
		})
	}

	if needsCSI {
		tasks = append(tasks, notify.ProgressTask{
			Name: "csi",
			Fn: func(taskCtx context.Context) error {
				return create.InstallCSISilent(taskCtx, clusterCfg, factories)
			},
		})
	}

	if needsCertManager {
		tasks = append(tasks, notify.ProgressTask{
			Name: "cert-manager",
			Fn: func(taskCtx context.Context) error {
				return create.InstallCertManagerSilent(taskCtx, clusterCfg, factories)
			},
		})
	}

	if needsArgoCD {
		tasks = append(tasks, notify.ProgressTask{
			Name: "argocd",
			Fn: func(taskCtx context.Context) error {
				return create.InstallArgoCDSilent(taskCtx, clusterCfg, factories)
			},
		})
	}

	if needsFlux {
		tasks = append(tasks, notify.ProgressTask{
			Name: "flux",
			Fn: func(taskCtx context.Context) error {
				return create.InstallFluxSilent(taskCtx, clusterCfg, factories)
			},
		})
	}

	progressGroup := notify.NewProgressGroup(
		"Installing components",
		"📦",
		cmd.OutOrStdout(),
		notify.WithLabels(notify.InstallingLabels()),
		notify.WithTimer(tmr),
	)

	executeErr := progressGroup.Run(ctx, tasks...)
	if executeErr != nil {
		return fmt.Errorf("failed to execute parallel component installation: %w", executeErr)
	}

	return nil
}

func configureGitOpsResources(
	ctx context.Context,
	cmd *cobra.Command,
	clusterCfg *v1alpha1.Cluster,
	factories *create.InstallerFactories,
	needsArgoCD, needsFlux bool,
	gitOpsKubeconfig string,
) error {
	// Post-install GitOps configuration
	if needsArgoCD {
		notify.WriteMessage(notify.Message{
			Type:    notify.ActivityType,
			Content: argoCDResourcesActivity,
			Writer:  cmd.OutOrStdout(),
		})

		err := factories.EnsureArgoCDResources(ctx, gitOpsKubeconfig, clusterCfg)
		if err != nil {
			return fmt.Errorf("failed to configure Argo CD resources: %w", err)
		}

		notify.WriteMessage(notify.Message{
			Type:    notify.InfoType,
			Content: "Access ArgoCD UI at https://localhost:8080 via: kubectl port-forward svc/argocd-server -n argocd 8080:443",
			Writer:  cmd.OutOrStdout(),
		})
	}

	if needsFlux {
		notify.WriteMessage(notify.Message{
			Type:    notify.ActivityType,
			Content: fluxResourcesActivity,
			Writer:  cmd.OutOrStdout(),
		})

		err := factories.EnsureFluxResources(ctx, gitOpsKubeconfig, clusterCfg)
		if err != nil {
			return fmt.Errorf("failed to configure Flux resources: %w", err)
		}
	}

	return nil
}
