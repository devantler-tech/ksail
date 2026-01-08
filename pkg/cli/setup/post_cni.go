package setup

import (
	"context"
	"fmt"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	k3dconfigmanager "github.com/devantler-tech/ksail/v5/pkg/io/config-manager/k3d"
	kindconfigmanager "github.com/devantler-tech/ksail/v5/pkg/io/config-manager/kind"
	talosconfigmanager "github.com/devantler-tech/ksail/v5/pkg/io/config-manager/talos"
	"github.com/devantler-tech/ksail/v5/pkg/utils/notify"
	"github.com/devantler-tech/ksail/v5/pkg/utils/timer"
	"github.com/spf13/cobra"
)

const (
	fluxResourcesActivity   = "applying custom resources"
	argoCDResourcesActivity = "configuring argocd resources"
)

// resolveClusterNameFromContext resolves the cluster name from the cluster config.
// This uses the distribution's default cluster name for registry naming.
// The cluster name is used for constructing registry container names (e.g., k3d-default-local-registry).
func resolveClusterNameFromContext(clusterCfg *v1alpha1.Cluster) string {
	if clusterCfg == nil {
		return kindconfigmanager.DefaultClusterName
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
		return kindconfigmanager.DefaultClusterName
	case v1alpha1.DistributionTalos:
		return talosconfigmanager.DefaultClusterName
	default:
		return kindconfigmanager.DefaultClusterName
	}
}

// ComponentRequirements represents which components need to be installed.
type ComponentRequirements struct {
	NeedsMetricsServer      bool
	NeedsKubeletCSRApprover bool
	NeedsCSI                bool
	NeedsCertManager        bool
	NeedsPolicyEngine       bool
	NeedsArgoCD             bool
	NeedsFlux               bool
}

// Count returns the number of components that need to be installed.
func (r ComponentRequirements) Count() int {
	count := 0
	if r.NeedsMetricsServer {
		count++
	}

	if r.NeedsKubeletCSRApprover {
		count++
	}

	if r.NeedsCSI {
		count++
	}

	if r.NeedsCertManager {
		count++
	}

	if r.NeedsPolicyEngine {
		count++
	}

	if r.NeedsArgoCD {
		count++
	}

	if r.NeedsFlux {
		count++
	}

	return count
}

// GetComponentRequirements determines which components need to be installed based on cluster config.
func GetComponentRequirements(clusterCfg *v1alpha1.Cluster) ComponentRequirements {
	needsMetricsServer := NeedsMetricsServerInstall(clusterCfg)

	// For Talos, the kubelet-csr-approver is installed during bootstrap via extraManifests,
	// so we skip the Helm-based installation. For other distributions, we install it via Helm.
	needsKubeletCSRApprover := needsMetricsServer &&
		clusterCfg.Spec.Cluster.Distribution != v1alpha1.DistributionTalos

	return ComponentRequirements{
		NeedsMetricsServer:      needsMetricsServer,
		NeedsKubeletCSRApprover: needsKubeletCSRApprover,
		NeedsCSI:                clusterCfg.Spec.Cluster.CSI == v1alpha1.CSILocalPathStorage,
		NeedsCertManager:        clusterCfg.Spec.Cluster.CertManager == v1alpha1.CertManagerEnabled,
		NeedsPolicyEngine:       clusterCfg.Spec.Cluster.PolicyEngine != v1alpha1.PolicyEngineNone,
		NeedsArgoCD:             clusterCfg.Spec.Cluster.GitOpsEngine == v1alpha1.GitOpsEngineArgoCD,
		NeedsFlux:               clusterCfg.Spec.Cluster.GitOpsEngine == v1alpha1.GitOpsEngineFlux,
	}
}

// InstallPostCNIComponents installs all post-CNI components in parallel.
// This includes metrics-server, CSI, cert-manager, and GitOps engines (Flux/ArgoCD).
func InstallPostCNIComponents(
	cmd *cobra.Command,
	clusterCfg *v1alpha1.Cluster,
	factories *InstallerFactories,
	tmr timer.Timer,
) error {
	reqs := GetComponentRequirements(clusterCfg)

	if reqs.Count() == 0 {
		return nil
	}

	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	var (
		gitOpsKubeconfig    string
		gitOpsKubeconfigErr error
	)

	if reqs.NeedsArgoCD || reqs.NeedsFlux {
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
	reqs ComponentRequirements,
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
	reqs ComponentRequirements,
) []notify.ProgressTask {
	var tasks []notify.ProgressTask

	if reqs.NeedsMetricsServer {
		tasks = append(
			tasks,
			newTask("metrics-server", clusterCfg, factories, InstallMetricsServerSilent),
		)
	}

	if reqs.NeedsKubeletCSRApprover {
		tasks = append(
			tasks,
			newTask("kubelet-csr-approver", clusterCfg, factories, InstallKubeletCSRApproverSilent),
		)
	}

	if reqs.NeedsCSI {
		tasks = append(tasks, newTask("csi", clusterCfg, factories, InstallCSISilent))
	}

	if reqs.NeedsCertManager {
		tasks = append(
			tasks,
			newTask("cert-manager", clusterCfg, factories, InstallCertManagerSilent),
		)
	}

	if reqs.NeedsPolicyEngine {
		tasks = append(
			tasks,
			newTask("policy-engine", clusterCfg, factories, InstallPolicyEngineSilent),
		)
	}

	if reqs.NeedsArgoCD {
		tasks = append(tasks, newTask("argocd", clusterCfg, factories, InstallArgoCDSilent))
	}

	if reqs.NeedsFlux {
		tasks = append(tasks, newTask("flux", clusterCfg, factories, InstallFluxSilent))
	}

	return tasks
}

type silentInstallFunc func(ctx context.Context, cfg *v1alpha1.Cluster, f *InstallerFactories) error

func newTask(
	name string,
	cfg *v1alpha1.Cluster,
	factories *InstallerFactories,
	fn silentInstallFunc,
) notify.ProgressTask {
	return notify.ProgressTask{
		Name: name,
		Fn:   func(ctx context.Context) error { return fn(ctx, cfg, factories) },
	}
}

func configureGitOpsResources(
	ctx context.Context,
	cmd *cobra.Command,
	clusterCfg *v1alpha1.Cluster,
	factories *InstallerFactories,
	reqs ComponentRequirements,
	gitOpsKubeconfig string,
) error {
	// Only show configure stage if there are GitOps resources to configure
	if !reqs.NeedsArgoCD && !reqs.NeedsFlux {
		return nil
	}

	// Resolve cluster name for registry naming
	clusterName := resolveClusterNameFromContext(clusterCfg)

	// Show title for configure stage
	notify.WriteMessage(notify.Message{
		Type:    notify.TitleType,
		Content: "Configuring components...",
		Emoji:   "⚙️",
		Writer:  cmd.OutOrStdout(),
	})

	// Post-install GitOps configuration
	if reqs.NeedsArgoCD {
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

	if reqs.NeedsFlux {
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

	// Show success message for configure stage
	notify.WriteMessage(notify.Message{
		Type:    notify.SuccessType,
		Content: "components configured",
		Writer:  cmd.OutOrStdout(),
	})

	return nil
}
