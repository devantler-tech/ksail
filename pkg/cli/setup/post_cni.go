package setup

import (
	"context"
	"fmt"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	k3dconfigmanager "github.com/devantler-tech/ksail/v5/pkg/io/config-manager/k3d"
	talosconfigmanager "github.com/devantler-tech/ksail/v5/pkg/io/config-manager/talos"
	"github.com/devantler-tech/ksail/v5/pkg/utils/notify"
	"github.com/devantler-tech/ksail/v5/pkg/utils/timer"
	"github.com/spf13/cobra"
)

const (
	fluxResourcesActivity   = "applying custom resources"
	argoCDResourcesActivity = "configuring argocd resources"

	// kindDefaultClusterName is the default cluster name for Kind clusters.
	// Kind uses "kind" as the default cluster name when not specified.
	kindDefaultClusterName = "kind"
)

// resolveClusterNameFromContext resolves the cluster name from the cluster config.
// This uses the distribution's default cluster name for registry naming.
// The cluster name is used for constructing registry container names (e.g., k3d-default-local-registry).
func resolveClusterNameFromContext(clusterCfg *v1alpha1.Cluster) string {
	if clusterCfg == nil {
		return kindDefaultClusterName
	}

	return resolveDefaultClusterName(clusterCfg.Spec.Cluster.Distribution)
}

// resolveDefaultClusterName returns the default cluster name for a given distribution.
// This matches the naming conventions used by each distribution's provisioner.
func resolveDefaultClusterName(distribution v1alpha1.Distribution) string {
	switch distribution {
	case v1alpha1.DistributionK3d:
		return k3dconfigmanager.DefaultClusterName
	case v1alpha1.DistributionKind:
		return kindDefaultClusterName
	case v1alpha1.DistributionTalos:
		return talosconfigmanager.DefaultClusterName
	default:
		return kindDefaultClusterName
	}
}

type componentRequirements struct {
	needsMetricsServer bool
	needsCSI           bool
	needsCertManager   bool
	needsArgoCD        bool
	needsFlux          bool
}

func (r componentRequirements) count() int {
	count := 0
	if r.needsMetricsServer {
		count++
	}

	if r.needsCSI {
		count++
	}

	if r.needsCertManager {
		count++
	}

	if r.needsArgoCD {
		count++
	}

	if r.needsFlux {
		count++
	}

	return count
}

func getComponentRequirements(clusterCfg *v1alpha1.Cluster) componentRequirements {
	return componentRequirements{
		needsMetricsServer: NeedsMetricsServerInstall(clusterCfg),
		needsCSI:           clusterCfg.Spec.Cluster.CSI == v1alpha1.CSILocalPathStorage,
		needsCertManager:   clusterCfg.Spec.Cluster.CertManager == v1alpha1.CertManagerEnabled,
		needsArgoCD:        clusterCfg.Spec.Cluster.GitOpsEngine == v1alpha1.GitOpsEngineArgoCD,
		needsFlux:          clusterCfg.Spec.Cluster.GitOpsEngine == v1alpha1.GitOpsEngineFlux,
	}
}

// InstallPostCNIComponents installs all post-CNI components in parallel.
// This includes metrics-server, CSI, cert-manager, and GitOps engines (Flux/ArgoCD).
func InstallPostCNIComponents(
	cmd *cobra.Command,
	clusterCfg *v1alpha1.Cluster,
	factories *InstallerFactories,
	tmr timer.Timer,
	firstActivityShown *bool,
) error {
	reqs := getComponentRequirements(clusterCfg)

	if reqs.count() == 0 {
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

	if reqs.needsArgoCD || reqs.needsFlux {
		_, gitOpsKubeconfig, gitOpsKubeconfigErr = factories.HelmClientFactory(clusterCfg)
		if gitOpsKubeconfigErr != nil {
			return fmt.Errorf("failed to create helm client for gitops: %w", gitOpsKubeconfigErr)
		}
	}

	err := installComponentsInParallel(ctx, cmd, clusterCfg, factories, tmr, reqs)
	if err != nil {
		return err
	}

	return configureGitOpsResources(ctx, cmd, clusterCfg, factories, reqs, gitOpsKubeconfig)
}

func installComponentsInParallel(
	ctx context.Context,
	cmd *cobra.Command,
	clusterCfg *v1alpha1.Cluster,
	factories *InstallerFactories,
	tmr timer.Timer,
	reqs componentRequirements,
) error {
	tasks := buildComponentTasks(clusterCfg, factories, reqs)

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

func buildComponentTasks(
	clusterCfg *v1alpha1.Cluster,
	factories *InstallerFactories,
	reqs componentRequirements,
) []notify.ProgressTask {
	var tasks []notify.ProgressTask

	if reqs.needsMetricsServer {
		tasks = append(tasks, notify.ProgressTask{
			Name: "metrics-server",
			Fn: func(taskCtx context.Context) error {
				return InstallMetricsServerSilent(taskCtx, clusterCfg, factories)
			},
		})
	}

	if reqs.needsCSI {
		tasks = append(tasks, notify.ProgressTask{
			Name: "csi",
			Fn: func(taskCtx context.Context) error {
				return InstallCSISilent(taskCtx, clusterCfg, factories)
			},
		})
	}

	if reqs.needsCertManager {
		tasks = append(tasks, notify.ProgressTask{
			Name: "cert-manager",
			Fn: func(taskCtx context.Context) error {
				return InstallCertManagerSilent(taskCtx, clusterCfg, factories)
			},
		})
	}

	if reqs.needsArgoCD {
		tasks = append(tasks, notify.ProgressTask{
			Name: "argocd",
			Fn: func(taskCtx context.Context) error {
				return InstallArgoCDSilent(taskCtx, clusterCfg, factories)
			},
		})
	}

	if reqs.needsFlux {
		tasks = append(tasks, notify.ProgressTask{
			Name: "flux",
			Fn: func(taskCtx context.Context) error {
				return InstallFluxSilent(taskCtx, clusterCfg, factories)
			},
		})
	}

	return tasks
}

func configureGitOpsResources(
	ctx context.Context,
	cmd *cobra.Command,
	clusterCfg *v1alpha1.Cluster,
	factories *InstallerFactories,
	reqs componentRequirements,
	gitOpsKubeconfig string,
) error {
	// Resolve cluster name for registry naming
	clusterName := resolveClusterNameFromContext(clusterCfg)

	// Post-install GitOps configuration
	if reqs.needsArgoCD {
		notify.WriteMessage(notify.Message{
			Type:    notify.ActivityType,
			Content: argoCDResourcesActivity,
			Writer:  cmd.OutOrStdout(),
		})

		err := factories.EnsureArgoCDResources(ctx, gitOpsKubeconfig, clusterCfg, clusterName)
		if err != nil {
			return fmt.Errorf("failed to configure Argo CD resources: %w", err)
		}

		notify.WriteMessage(notify.Message{
			Type:    notify.InfoType,
			Content: "Access ArgoCD UI at https://localhost:8080 via: kubectl port-forward svc/argocd-server -n argocd 8080:443",
			Writer:  cmd.OutOrStdout(),
		})
	}

	if reqs.needsFlux {
		notify.WriteMessage(notify.Message{
			Type:    notify.ActivityType,
			Content: fluxResourcesActivity,
			Writer:  cmd.OutOrStdout(),
		})

		err := factories.EnsureFluxResources(ctx, gitOpsKubeconfig, clusterCfg, clusterName)
		if err != nil {
			return fmt.Errorf("failed to configure Flux resources: %w", err)
		}
	}

	return nil
}
