package setup

import (
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/client/oci"
	kindconfigmanager "github.com/devantler-tech/ksail/v5/pkg/fsutil/configmanager/kind"
	"github.com/devantler-tech/ksail/v5/pkg/notify"
	clusterdetector "github.com/devantler-tech/ksail/v5/pkg/svc/detector/cluster"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/registry"
	registryhelpers "github.com/devantler-tech/ksail/v5/pkg/svc/registryresolver"
	"github.com/devantler-tech/ksail/v5/pkg/timer"
	"github.com/spf13/cobra"
)

const (
	fluxResourcesActivity   = "applying custom resources"
	argoCDResourcesActivity = "configuring argocd resources"
)

// ShouldPushOCIArtifact determines if OCI artifact push should happen for GitOps engines.
// Returns true if Flux or ArgoCD is enabled and a local registry is configured.
func ShouldPushOCIArtifact(clusterCfg *v1alpha1.Cluster) bool {
	// Only push for GitOps engines that consume OCI artifacts
	engine := clusterCfg.Spec.Cluster.GitOpsEngine
	if engine != v1alpha1.GitOpsEngineFlux && engine != v1alpha1.GitOpsEngineArgoCD {
		return false
	}

	// Only push if local registry is enabled
	return clusterCfg.Spec.Cluster.LocalRegistry.Enabled()
}

// resolveClusterNameFromContext resolves the cluster name from the cluster config.
// It first attempts to parse the cluster name from Connection.Context
// (e.g., "k3d-system-test-cluster" -> "system-test-cluster").
// Falls back to the distribution's default cluster name if context is not set or parsing fails.
// The cluster name is used for constructing registry container names
// (e.g., system-test-cluster-local-registry).
func resolveClusterNameFromContext(clusterCfg *v1alpha1.Cluster) string {
	if clusterCfg == nil {
		return kindconfigmanager.DefaultClusterName
	}

	// First try to extract cluster name from the context if available
	contextName := strings.TrimSpace(clusterCfg.Spec.Cluster.Connection.Context)
	if contextName != "" {
		_, clusterName, err := clusterdetector.DetectDistributionFromContext(contextName)
		if err == nil && clusterName != "" {
			return clusterName
		}
	}

	// Fall back to default cluster name for the distribution
	return clusterCfg.Spec.Cluster.Distribution.DefaultClusterName()
}

// ComponentRequirements represents which components need to be installed.
type ComponentRequirements struct {
	NeedsMetricsServer      bool
	NeedsLoadBalancer       bool
	NeedsKubeletCSRApprover bool
	NeedsCSI                bool
	NeedsCertManager        bool
	NeedsPolicyEngine       bool
	NeedsArgoCD             bool
	NeedsFlux               bool
}

// Count returns the number of components that need to be installed.
func (r ComponentRequirements) Count() int {
	components := []bool{
		r.NeedsMetricsServer,
		r.NeedsLoadBalancer,
		r.NeedsKubeletCSRApprover,
		r.NeedsCSI,
		r.NeedsCertManager,
		r.NeedsPolicyEngine,
		r.NeedsArgoCD,
		r.NeedsFlux,
	}

	count := 0

	for _, needed := range components {
		if needed {
			count++
		}
	}

	return count
}

// GetComponentRequirements determines which components need to be installed based on cluster config.
func GetComponentRequirements(clusterCfg *v1alpha1.Cluster) ComponentRequirements {
	needsMetricsServer := NeedsMetricsServerInstall(clusterCfg)

	// For Talos, the kubelet-serving-cert-approver is installed during bootstrap via extraManifests,
	// so we skip the Helm-based installation. For other distributions, we use postfinance/kubelet-csr-approver via Helm.
	needsKubeletCSRApprover := needsMetricsServer &&
		clusterCfg.Spec.Cluster.Distribution != v1alpha1.DistributionTalos

	return ComponentRequirements{
		NeedsMetricsServer:      needsMetricsServer,
		NeedsLoadBalancer:       NeedsLoadBalancerInstall(clusterCfg),
		NeedsKubeletCSRApprover: needsKubeletCSRApprover,
		NeedsCSI:                needsCSIInstall(clusterCfg),
		NeedsCertManager:        clusterCfg.Spec.Cluster.CertManager == v1alpha1.CertManagerEnabled,
		NeedsPolicyEngine:       clusterCfg.Spec.Cluster.PolicyEngine != v1alpha1.PolicyEngineNone,
		NeedsArgoCD:             clusterCfg.Spec.Cluster.GitOpsEngine == v1alpha1.GitOpsEngineArgoCD,
		NeedsFlux:               clusterCfg.Spec.Cluster.GitOpsEngine == v1alpha1.GitOpsEngineFlux,
	}
}

// needsCSIInstall determines if CSI needs to be installed.
//
// In general, we install CSI only when it is explicitly Enabled AND the
// distribution × provider combination does not provide it by default.
//
// Special case:
//   - Talos × Hetzner: Hetzner CSI is not pre-installed and must be installed
//     by KSail when CSI is either Default or Enabled.
func needsCSIInstall(clusterCfg *v1alpha1.Cluster) bool {
	dist := clusterCfg.Spec.Cluster.Distribution
	provider := clusterCfg.Spec.Cluster.Provider
	csiSetting := clusterCfg.Spec.Cluster.CSI

	// Special handling for Talos clusters on Hetzner:
	// According to the distribution × provider matrix, Hetzner CSI must be
	// installed by KSail for both Default and Enabled CSI settings.
	if dist == v1alpha1.DistributionTalos && provider == v1alpha1.ProviderHetzner {
		return csiSetting == v1alpha1.CSIDefault || csiSetting == v1alpha1.CSIEnabled
	}

	// Generic behavior for all other distribution × provider combinations.
	if csiSetting != v1alpha1.CSIEnabled {
		return false
	}

	// Don't install if distribution × provider provides it by default.
	return !dist.ProvidesCSIByDefault(provider)
}

// InstallPostCNIComponents installs all post-CNI components in parallel.
// This includes metrics-server, CSI, cert-manager, and GitOps engines (Flux/ArgoCD).
// For Flux, the OCI artifact push and readiness wait happens after installation.
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

	err := installComponentsInPhases(ctx, cmd, clusterCfg, factories, tmr, reqs)
	if err != nil {
		return err
	}

	return configureGitOpsResources(
		ctx,
		cmd,
		clusterCfg,
		factories,
		reqs,
		gitOpsKubeconfig,
	)
}

func installComponentsInPhases(
	ctx context.Context,
	cmd *cobra.Command,
	clusterCfg *v1alpha1.Cluster,
	factories *InstallerFactories,
	tmr timer.Timer,
	reqs ComponentRequirements,
) error {
	writer := cmd.OutOrStdout()
	labels := notify.InstallingLabels()

	// Phase 1: Infrastructure components (metrics-server, load-balancer,
	// kubelet-csr-approver, CSI, cert-manager, policy-engine).
	// Policy engines register webhooks that can interfere with API requests,
	// so they must be fully ready before GitOps engines start.
	infraTasks := buildInfrastructureTasks(clusterCfg, factories, reqs)
	if len(infraTasks) > 0 {
		infraGroup := notify.NewProgressGroup(
			"Installing infrastructure components",
			"📦",
			writer,
			notify.WithLabels(labels),
			notify.WithTimer(tmr),
		)

		err := infraGroup.Run(ctx, infraTasks...)
		if err != nil {
			return fmt.Errorf("failed to install infrastructure components: %w", err)
		}
	}

	// Phase 2: GitOps engines (ArgoCD, Flux).
	// Installed after infrastructure so policy-engine webhooks are stable
	// and won't cause intermittent kstatus watch timeouts.
	gitopsTasks := buildGitOpsTasks(clusterCfg, factories, reqs)
	if len(gitopsTasks) > 0 {
		gitopsGroup := notify.NewProgressGroup(
			"Installing GitOps engines",
			"📦",
			writer,
			notify.WithLabels(labels),
			notify.WithTimer(tmr),
		)

		err := gitopsGroup.Run(ctx, gitopsTasks...)
		if err != nil {
			return fmt.Errorf("failed to install GitOps engines: %w", err)
		}
	}

	return nil
}

// buildInfrastructureTasks returns tasks for infrastructure components that
// should be installed before GitOps engines. This includes policy engines
// whose webhooks must be fully ready before other Helm installations begin.
func buildInfrastructureTasks(
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

	if reqs.NeedsLoadBalancer {
		tasks = append(
			tasks,
			newTask("load-balancer", clusterCfg, factories, InstallLoadBalancerSilent),
		)
	}

	if reqs.NeedsKubeletCSRApprover {
		tasks = append(
			tasks,
			newTask("kubelet-csr-approver", clusterCfg, factories, installKubeletCSRApproverSilent),
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

	return tasks
}

// buildGitOpsTasks returns tasks for GitOps engines that should be installed
// after infrastructure components are ready.
func buildGitOpsTasks(
	clusterCfg *v1alpha1.Cluster,
	factories *InstallerFactories,
	reqs ComponentRequirements,
) []notify.ProgressTask {
	var tasks []notify.ProgressTask

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
	writer := cmd.OutOrStdout()

	// Show title for configure stage
	notify.WriteMessage(notify.Message{
		Type: notify.TitleType, Content: "Configuring components...", Emoji: "⚙️", Writer: writer,
	})

	// Post-install GitOps configuration
	if reqs.NeedsArgoCD {
		err := configureArgoCD(
			ctx,
			cmd,
			factories,
			gitOpsKubeconfig,
			clusterCfg,
			clusterName,
			writer,
		)
		if err != nil {
			return err
		}
	}

	if reqs.NeedsFlux {
		err := configureFlux(
			ctx,
			cmd,
			factories,
			gitOpsKubeconfig,
			clusterCfg,
			clusterName,
			writer,
		)
		if err != nil {
			return err
		}
	}

	// Show success message for configure stage
	notify.WriteMessage(
		notify.Message{Type: notify.SuccessType, Content: "components configured", Writer: writer},
	)

	return nil
}

func configureArgoCD(
	ctx context.Context,
	cmd *cobra.Command,
	factories *InstallerFactories,
	kubeconfig string,
	clusterCfg *v1alpha1.Cluster,
	clusterName string,
	writer io.Writer,
) error {
	// Ensure OCI artifact exists before creating the ArgoCD Application,
	// otherwise ArgoCD enters a ComparisonError loop that can saturate etcd.
	var err error
	if factories.EnsureOCIArtifact != nil {
		_, err = factories.EnsureOCIArtifact(ctx, cmd, clusterCfg, clusterName, writer)
	} else {
		_, err = ensureOCIArtifact(ctx, cmd, clusterCfg, clusterName, writer)
	}

	if err != nil {
		return fmt.Errorf("failed to ensure OCI artifact for ArgoCD: %w", err)
	}

	notify.WriteMessage(
		notify.Message{Type: notify.ActivityType, Content: argoCDResourcesActivity, Writer: writer},
	)

	err = factories.EnsureArgoCDResources(ctx, kubeconfig, clusterCfg, clusterName)
	if err != nil {
		return fmt.Errorf("failed to configure Argo CD resources: %w", err)
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.InfoType,
		Content: "Access ArgoCD UI at https://localhost:8080 via: kubectl port-forward svc/argocd-server -n argocd 8080:443",
		Writer:  writer,
	})

	return nil
}

func configureFlux(
	ctx context.Context,
	cmd *cobra.Command,
	factories *InstallerFactories,
	kubeconfig string,
	clusterCfg *v1alpha1.Cluster,
	clusterName string,
	writer io.Writer,
) error {
	notify.WriteMessage(
		notify.Message{Type: notify.ActivityType, Content: fluxResourcesActivity, Writer: writer},
	)

	// Step 1: Setup FluxInstance CR (does not wait for readiness)
	err := factories.SetupFluxInstance(ctx, kubeconfig, clusterCfg, clusterName)
	if err != nil {
		return fmt.Errorf("failed to setup FluxInstance: %w", err)
	}

	// Step 2: Check if OCI artifact exists and push if needed
	// Use the factory function if provided (for testing), otherwise use default
	var artifactPushed bool
	if factories.EnsureOCIArtifact != nil {
		artifactPushed, err = factories.EnsureOCIArtifact(ctx, cmd, clusterCfg, clusterName, writer)
	} else {
		artifactPushed, err = ensureOCIArtifact(ctx, cmd, clusterCfg, clusterName, writer)
	}

	if err != nil {
		return fmt.Errorf("failed to ensure OCI artifact: %w", err)
	}

	// Step 3: Wait for FluxInstance to be ready (only if artifact was pushed/exists)
	if artifactPushed {
		notify.WriteMessage(
			notify.Message{
				Type:    notify.ActivityType,
				Content: "waiting for flux to be ready",
				Writer:  writer,
			},
		)

		err = factories.WaitForFluxReady(ctx, kubeconfig)
		if err != nil {
			return fmt.Errorf("failed waiting for Flux to be ready: %w", err)
		}
	}

	return nil
}

// ensureOCIArtifact checks if an OCI artifact exists and pushes one if needed.
// Returns true if an artifact exists or was pushed, false if no artifact needed.
func ensureOCIArtifact(
	ctx context.Context,
	_ *cobra.Command,
	clusterCfg *v1alpha1.Cluster,
	clusterName string,
	writer io.Writer,
) (bool, error) {
	// Only check/push for local registries
	if !clusterCfg.Spec.Cluster.LocalRegistry.Enabled() {
		return false, nil
	}

	// Resolve registry info
	registryInfo, err := registryhelpers.ResolveRegistry(
		ctx,
		registryhelpers.ResolveRegistryOptions{
			ClusterConfig: clusterCfg,
			ClusterName:   clusterName,
		},
	)
	if err != nil {
		return false, fmt.Errorf("resolve registry: %w", err)
	}

	// Build the artifact reference details
	artifactOpts := buildArtifactExistsOptions(registryInfo, clusterCfg)

	// Check if artifact already exists
	verifier := oci.NewRegistryVerifier()

	exists, err := verifier.ArtifactExists(ctx, artifactOpts)
	if err != nil {
		// Log warning but continue - we'll try to push
		notify.WriteMessage(notify.Message{
			Type:    notify.ActivityType,
			Content: "checking for existing artifact",
			Writer:  writer,
		})
	}

	if exists {
		// Artifact already exists, no need to push
		return true, nil
	}

	return pushInitialOCIArtifact(ctx, clusterCfg, clusterName, writer)
}

// pushInitialOCIArtifact pushes an initial OCI artifact when none exists.
func pushInitialOCIArtifact(
	ctx context.Context,
	clusterCfg *v1alpha1.Cluster,
	clusterName string,
	writer io.Writer,
) (bool, error) {
	// Artifact doesn't exist, push an empty one
	notify.WriteMessage(notify.Message{
		Type:    notify.ActivityType,
		Content: "pushing initial oci artifact",
		Writer:  writer,
	})

	result, err := registryhelpers.PushOCIArtifact(ctx, registryhelpers.PushOCIArtifactOptions{
		ClusterConfig: clusterCfg,
		ClusterName:   clusterName,
		SourceDir:     "", // Use default from config
		Ref:           "", // Use default tag
		Validate:      clusterCfg.Spec.Workload.ValidateOnPush,
	})
	if err != nil {
		return false, fmt.Errorf("push oci artifact: %w", err)
	}

	if result.Empty {
		notify.WriteMessage(notify.Message{
			Type:    notify.InfoType,
			Content: "pushed empty kustomization (source directory not found)",
			Writer:  writer,
		})
	}

	return result.Pushed, nil
}

// buildArtifactExistsOptions creates options for checking artifact existence.
func buildArtifactExistsOptions(
	registryInfo *registryhelpers.Info,
	clusterCfg *v1alpha1.Cluster,
) oci.ArtifactExistsOptions {
	return oci.ArtifactExistsOptions{
		RegistryEndpoint: resolveRegistryEndpoint(registryInfo),
		Repository:       resolveRepository(registryInfo, clusterCfg),
		Tag:              resolveTag(registryInfo),
		Username:         registryInfo.Username,
		Password:         registryInfo.Password,
		Insecure:         !clusterCfg.Spec.Cluster.LocalRegistry.IsExternal(),
	}
}

func resolveRegistryEndpoint(info *registryhelpers.Info) string {
	if info.Port > 0 {
		return net.JoinHostPort(info.Host, strconv.Itoa(int(info.Port)))
	}

	return info.Host
}

func resolveRepository(info *registryhelpers.Info, cfg *v1alpha1.Cluster) string {
	if info.Repository != "" {
		return info.Repository
	}

	sourceDir := cfg.Spec.Workload.SourceDirectory
	if sourceDir == "" {
		return v1alpha1.DefaultSourceDirectory
	}

	return sourceDir
}

func resolveTag(info *registryhelpers.Info) string {
	if info.Tag != "" {
		return info.Tag
	}

	return registry.DefaultLocalArtifactTag
}
