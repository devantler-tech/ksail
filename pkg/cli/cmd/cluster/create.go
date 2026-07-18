package cluster

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/clusterflags"
	"github.com/devantler-tech/ksail/v7/pkg/cli/flags"
	"github.com/devantler-tech/ksail/v7/pkg/cli/lifecycle"
	"github.com/devantler-tech/ksail/v7/pkg/cli/setup"
	"github.com/devantler-tech/ksail/v7/pkg/cli/setup/localregistry"
	"github.com/devantler-tech/ksail/v7/pkg/cli/setup/mirrorregistry"
	configmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager"
	k3dconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/k3d"
	kindconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/kind"
	ksailconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/ksail"
	talosconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
	"github.com/devantler-tech/ksail/v7/pkg/notify"
	imagesvc "github.com/devantler-tech/ksail/v7/pkg/svc/image"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/svc/state"
	"github.com/devantler-tech/ksail/v7/pkg/timer"
	"github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	"github.com/spf13/cobra"
)

const (
	k3sDisableMetricsServerFlag = "--disable=metrics-server"
	k3sDisableLocalStorageFlag  = "--disable=local-storage"
	k3sDisableServiceLBFlag     = "--disable=servicelb"
	k3sDisableTraefikFlag       = "--disable=traefik"
	k3sFlanelBackendNoneFlag    = "--flannel-backend=none"
	k3sDisableNetworkPolicyFlag = "--disable-network-policy"
	// k3dAllServersNodeFilter targets all server (control-plane) nodes when applying K3s extra args.
	k3dAllServersNodeFilter = "server:*"
)

// newCreateLifecycleConfig creates the lifecycle configuration for cluster creation.
func newCreateLifecycleConfig() lifecycle.Config {
	return lifecycle.Config{
		TitleEmoji:         "🚀",
		TitleContent:       "Create cluster...",
		ActivityContent:    "creating cluster",
		SuccessContent:     "cluster created",
		ErrorMessagePrefix: "failed to create cluster",
		Action: func(ctx context.Context, provisioner clusterprovisioner.Provisioner, clusterName string) error {
			return provisioner.Create(ctx, clusterName)
		},
	}
}

// NewCreateCmd wires the cluster create command.
func NewCreateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "create",
		Short:        "Create a cluster",
		Long:         `Create a Kubernetes cluster as defined by configuration.`,
		SilenceUsage: true,
		Annotations: map[string]string{
			annotations.AnnotationPermission: permissionWrite,
		},
	}

	cfgManager := setupMutationCmdFlags(cmd)

	cmd.Flags().String("ttl", "",
		"Auto-destroy cluster after duration (e.g. 1h, 30m, 2h30m). If not set, cluster persists indefinitely.")

	cmd.RunE = lifecycle.WrapHandler(cfgManager, handleCreateRunE)

	return cmd
}

// handleCreateRunE executes cluster creation with mirror registry setup and CNI installation.
func handleCreateRunE(
	cmd *cobra.Command,
	cfgManager *ksailconfigmanager.ConfigManager,
	deps lifecycle.Deps,
) error {
	deps.Timer.Start()

	ctx, clusterName, err := loadAndValidateClusterConfig(cfgManager, deps)
	if err != nil {
		return err
	}

	err = validateEKSMutationConfigSource(ctx)
	if err != nil {
		return err
	}

	clusterflags.ApplyClusterMutationFlags(cmd, ctx.ClusterCfg)

	err = validatePostMutationFlags(ctx)
	if err != nil {
		return err
	}

	err = runClusterCreationWorkflow(cmd, cfgManager, ctx, deps)
	if err != nil {
		return err
	}

	// Persist the ClusterSpec so that future updates have an accurate baseline
	// for fields that cannot be detected from the live cluster (e.g., Talos ISO).
	saveErr := state.SaveClusterSpec(clusterName, &ctx.ClusterCfg.Spec.Cluster)
	if saveErr != nil {
		notify.Warningf(cmd.OutOrStderr(), "failed to save cluster state: %v", saveErr)
	}

	return maybeWaitForTTL(cmd, clusterName, ctx.ClusterCfg, ctx.EKSConfig)
}

// newProvisionerFactory returns the cluster provisioner factory, using any test override if set.
// It is the single construction point for the default factory: every command path that needs a
// provisioner (create, update, diff, recreate) must go through it so the distribution config
// stays complete for all distributions.
func newProvisionerFactory(ctx *localregistry.Context) clusterprovisioner.Factory {
	clusterProvisionerFactoryMu.RLock()

	factoryOverride := clusterProvisionerFactoryOverride

	clusterProvisionerFactoryMu.RUnlock()

	if factoryOverride != nil {
		return factoryOverride
	}

	return defaultProvisionerFactory(ctx)
}

// defaultProvisionerFactory builds the default cluster provisioner factory from the
// pre-loaded distribution configs on the context. Every distribution config field must
// be populated here — omitting one breaks provisioner creation for that distribution.
func defaultProvisionerFactory(ctx *localregistry.Context) clusterprovisioner.DefaultFactory {
	return clusterprovisioner.DefaultFactory{
		AWSResolution:        ctx.AWSResolution,
		AWSOwnershipVerifier: ctx.AWSOwnershipVerifier,
		DistributionConfig: &clusterprovisioner.DistributionConfig{
			Kind:        ctx.KindConfig,
			K3d:         ctx.K3dConfig,
			Talos:       ctx.TalosConfig,
			VCluster:    ctx.VClusterConfig,
			KWOK:        ctx.KWOKConfig,
			EKS:         ctx.EKSConfig,
			GKE:         ctx.GKEConfig,
			AKS:         ctx.AKSConfig,
			MirrorSpecs: ctx.MirrorSpecs,
		},
	}
}

// configureProvisionerFactory sets up the cluster provisioner factory on deps.
// Uses test override if available, otherwise creates a default factory.
func configureProvisionerFactory(
	deps *lifecycle.Deps,
	ctx *localregistry.Context,
) {
	deps.Factory = newProvisionerFactory(ctx)
}

// resolveNestedMirrorSpecs resolves registry mirror specs for the Kubernetes provider
// and stores them on the context so the nested provisioner can set them up inside the
// DinD environment. The host-level mirror stages are skipped for this provider
// (NeedsLocalDocker is false), so nested clusters would otherwise pull images
// anonymously and hit registry rate limits during boot. No-op for other providers.
func resolveNestedMirrorSpecs(
	cmd *cobra.Command,
	cfgManager *ksailconfigmanager.ConfigManager,
	ctx *localregistry.Context,
) error {
	if ctx.ClusterCfg.Spec.Cluster.Provider != v1alpha1.ProviderKubernetes {
		return nil
	}

	specs, err := mirrorregistry.ResolveMirrorSpecs(
		cmd,
		cfgManager,
		ctx.ClusterCfg,
		ctx.TalosConfig,
	)
	if err != nil {
		return fmt.Errorf("failed to resolve nested mirror specs: %w", err)
	}

	ctx.MirrorSpecs = specs

	return nil
}

// maybeImportCachedImages imports cached container images if configured.
// Logs warnings but does not fail cluster creation on import errors.
func maybeImportCachedImages(
	cmd *cobra.Command,
	ctx *localregistry.Context,
	tmr timer.Timer,
) {
	importPath := ctx.ClusterCfg.Spec.Cluster.ImportImages
	if importPath == "" {
		return
	}

	// Image import is not supported for Talos and VCluster clusters
	if ctx.ClusterCfg.Spec.Cluster.Distribution == v1alpha1.DistributionTalos ||
		ctx.ClusterCfg.Spec.Cluster.Distribution == v1alpha1.DistributionVCluster {
		notify.WriteMessage(notify.Message{
			Type:    notify.WarningType,
			Content: "image import is not supported for %s clusters; ignoring --import-images value %q",
			Args:    []any{ctx.ClusterCfg.Spec.Cluster.Distribution, importPath},
			Writer:  cmd.OutOrStderr(),
		})

		return
	}

	err := importCachedImages(cmd, ctx, importPath, tmr)
	if err != nil {
		notify.WriteMessage(notify.Message{
			Type:    notify.WarningType,
			Content: "failed to import images from %s: %v",
			Args:    []any{importPath, err},
			Writer:  cmd.OutOrStderr(),
		})
	}
}

func loadClusterConfiguration(
	cfgManager *ksailconfigmanager.ConfigManager,
	tmr timer.Timer,
) (*localregistry.Context, error) {
	// Load config to populate cfgManager.Config and cfgManager.DistributionConfig
	// The returned config is cached in cfgManager.Config, which is used by NewContextFromConfigManager
	_, err := cfgManager.Load(configmanager.LoadOptions{Timer: tmr})
	if err != nil {
		return nil, fmt.Errorf("failed to load cluster configuration: %w", err)
	}

	// Create context from the now-populated config manager
	return localregistry.NewContextFromConfigManager(cfgManager), nil
}

// buildRegistryStageParams creates a StageParams struct for registry operations.
// This helper reduces code duplication when calling registry stage functions.
func buildRegistryStageParams(
	cmd *cobra.Command,
	ctx *localregistry.Context,
	deps lifecycle.Deps,
	cfgManager *ksailconfigmanager.ConfigManager,
	localDeps localregistry.Dependencies,
) mirrorregistry.StageParams {
	return mirrorregistry.StageParams{
		Cmd:            cmd,
		ClusterCfg:     ctx.ClusterCfg,
		Deps:           deps,
		CfgManager:     cfgManager,
		KindConfig:     ctx.KindConfig,
		K3dConfig:      ctx.K3dConfig,
		TalosConfig:    ctx.TalosConfig,
		VClusterConfig: ctx.VClusterConfig,
		DockerInvoker:  localDeps.DockerInvoker,
	}
}

func validateRegistryForProvider(ctx *localregistry.Context) error {
	provider := ctx.ClusterCfg.Spec.Cluster.Provider

	registry := ctx.ClusterCfg.Spec.Cluster.LocalRegistry
	if provider.IsCloud() && registry.Enabled() && !registry.IsExternal() {
		return localregistry.ErrCloudProviderRequiresExternalRegistry
	}

	return nil
}

func ensureLocalRegistriesReady(
	cmd *cobra.Command,
	ctx *localregistry.Context,
	deps lifecycle.Deps,
	cfgManager *ksailconfigmanager.ConfigManager,
	localDeps localregistry.Dependencies,
) error {
	provider := ctx.ClusterCfg.Spec.Cluster.Provider

	// Cloud providers cannot use a Docker-based local registry — reject early with a clear error.
	err := validateRegistryForProvider(ctx)
	if err != nil {
		return err
	}

	if provider.NeedsLocalDocker() {
		// Stage 1: Provision local registry (skipped for external registries)
		err := localregistry.ExecuteStage(
			cmd,
			ctx,
			deps,
			localregistry.StageProvision,
			localDeps,
		)
		if err != nil {
			return fmt.Errorf("failed to provision local registry: %w", err)
		}
	}

	// Stage 2: Verify registry access.
	// Called unconditionally here, but VerifyRegistryAccess returns early unless an enabled
	// external registry is configured, in which case it validates any required registry auth.
	err = localregistry.VerifyRegistryAccess(cmd, ctx.ClusterCfg, deps)
	if err != nil {
		return fmt.Errorf("failed to verify registry access: %w", err)
	}

	if provider.NeedsLocalDocker() {
		params := buildRegistryStageParams(cmd, ctx, deps, cfgManager, localDeps)

		// Stage 3: Create and configure registry containers (local + mirrors)
		err = mirrorregistry.SetupRegistries(params)
		if err != nil {
			return fmt.Errorf("failed to setup registries: %w", err)
		}

		// Stage 4: Create Docker network
		err = mirrorregistry.CreateNetwork(params)
		if err != nil {
			return fmt.Errorf("failed to create docker network: %w", err)
		}

		// Stage 5: Connect registries to network (before cluster creation)
		err = mirrorregistry.ConnectRegistriesToNetwork(params)
		if err != nil {
			return fmt.Errorf("failed to connect registries to network: %w", err)
		}
	}

	return nil
}

func executeClusterLifecycle(
	cmd *cobra.Command,
	clusterCfg *v1alpha1.Cluster,
	deps lifecycle.Deps,
) error {
	deps.Timer.NewStage()

	err := lifecycle.RunWithConfig(cmd, deps, newCreateLifecycleConfig(), clusterCfg)
	if err != nil {
		return fmt.Errorf("failed to execute cluster lifecycle: %w", err)
	}

	return nil
}

func configureRegistryMirrorsInClusterWithWarning(
	cmd *cobra.Command,
	ctx *localregistry.Context,
	deps lifecycle.Deps,
	cfgManager *ksailconfigmanager.ConfigManager,
	localDeps localregistry.Dependencies,
) {
	params := buildRegistryStageParams(cmd, ctx, deps, cfgManager, localDeps)

	// Configure containerd inside cluster nodes to use registry mirrors (Kind only)
	err := mirrorregistry.ConfigureRegistryMirrorsInCluster(params)
	if err != nil {
		notify.WriteMessage(notify.Message{
			Type:    notify.ErrorType,
			Content: fmt.Sprintf("failed to configure registry mirrors in cluster: %v", err),
			Writer:  cmd.OutOrStdout(),
		})
	}
}

// handlePostCreationSetup installs CNI, CSI, cert-manager, metrics-server, and GitOps engines.
func handlePostCreationSetup(
	cmd *cobra.Command,
	clusterCfg *v1alpha1.Cluster,
	tmr timer.Timer,
) error {
	cniInstalled, err := setup.InstallCNI(cmd, clusterCfg, tmr)
	if err != nil {
		return fmt.Errorf("failed to install CNI: %w", err)
	}

	factories := getInstallerFactories()
	outputTimer := flags.MaybeTimer(cmd, tmr)

	// OCI artifact push is now handled inside InstallPostCNIComponents after Flux is installed
	err = setup.InstallPostCNIComponents(
		cmd,
		clusterCfg,
		factories,
		outputTimer,
		cniInstalled,
	)
	if err != nil {
		return fmt.Errorf("failed to install post-CNI components: %w", err)
	}

	// Configure OIDC kubeconfig entries when OIDC is enabled
	if clusterCfg.Spec.Cluster.OIDC.Enabled() {
		oidcErr := configureOIDCKubeconfig(cmd, clusterCfg)
		if oidcErr != nil {
			notify.WriteMessage(notify.Message{
				Type:    notify.WarningType,
				Content: "failed to configure OIDC kubeconfig: %v",
				Args:    []any{oidcErr},
				Writer:  cmd.OutOrStderr(),
			})
		}
	}

	return nil
}

// maybeDisableK3dFeature conditionally appends a K3s --disable flag to K3d config.
// It is a no-op when the distribution is not K3s, k3dConfig is nil, the feature
// is not in the disabled state, or the flag is already present.
func maybeDisableK3dFeature(
	clusterCfg *v1alpha1.Cluster,
	k3dConfig *v1alpha5.SimpleConfig,
	isDisabled bool,
	flag string,
) {
	if clusterCfg.Spec.Cluster.Distribution != v1alpha1.DistributionK3s || k3dConfig == nil {
		return
	}

	if !isDisabled {
		return
	}

	if hasK3sArg(k3dConfig, flag) {
		return
	}

	k3dConfig.Options.K3sOptions.ExtraArgs = append(
		k3dConfig.Options.K3sOptions.ExtraArgs,
		v1alpha5.K3sArgWithNodeFilters{
			Arg:         flag,
			NodeFilters: []string{k3dAllServersNodeFilter},
		},
	)
}

// setupK3dCNI configures K3d to disable flannel and network policy when a non-default
// CNI (Cilium or Calico) is selected. Without this, K3s starts with flannel enabled,
// causing conflicts when the custom CNI is installed post-creation.
// When Cilium is selected, K3s's built-in Traefik is also disabled because Cilium
// installs Gateway API CRDs that conflict with Traefik's CRD ownership on install.
func setupK3dCNI(clusterCfg *v1alpha1.Cluster, k3dConfig *v1alpha5.SimpleConfig) {
	if clusterCfg.Spec.Cluster.Distribution != v1alpha1.DistributionK3s || k3dConfig == nil {
		return
	}

	cni := clusterCfg.Spec.Cluster.CNI
	if cni != v1alpha1.CNICilium && cni != v1alpha1.CNICalico {
		return
	}

	for _, flag := range []string{k3sFlanelBackendNoneFlag, k3sDisableNetworkPolicyFlag} {
		if !hasK3sArgForServers(k3dConfig, flag) {
			k3dConfig.Options.K3sOptions.ExtraArgs = append(
				k3dConfig.Options.K3sOptions.ExtraArgs,
				v1alpha5.K3sArgWithNodeFilters{
					Arg:         flag,
					NodeFilters: []string{k3dAllServersNodeFilter},
				},
			)
		}
	}

	// Cilium installs Gateway API CRDs (e.g. backendtlspolicies.gateway.networking.k8s.io)
	// that conflict with K3s's built-in Traefik chart which claims ownership of the same CRDs.
	// Disabling Traefik avoids the CRD ownership conflict and the resulting CrashLoopBackOff.
	// We check specifically for a server-scoped entry because an agent-scoped entry would not
	// suppress Traefik on control-plane nodes.
	if cni == v1alpha1.CNICilium && !hasK3sArgForServers(k3dConfig, k3sDisableTraefikFlag) {
		k3dConfig.Options.K3sOptions.ExtraArgs = append(
			k3dConfig.Options.K3sOptions.ExtraArgs,
			v1alpha5.K3sArgWithNodeFilters{
				Arg:         k3sDisableTraefikFlag,
				NodeFilters: []string{k3dAllServersNodeFilter},
			},
		)
	}
}

// hasK3sArg checks whether a K3s arg flag is already present in the K3d config,
// regardless of the entry's node filters. Use hasK3sArgForServers when the
// pre-existing entry must cover all server nodes.
func hasK3sArg(k3dConfig *v1alpha5.SimpleConfig, flag string) bool {
	for _, arg := range k3dConfig.Options.K3sOptions.ExtraArgs {
		if arg.Arg == flag {
			return true
		}
	}

	return false
}

// hasK3sArgForServers checks whether a K3s arg flag is already present in the K3d config
// with node filters that cover all server nodes: either an empty filter list (applies to all
// nodes) or a filter that is exactly "server:*". A filter like "server:0" is intentionally
// excluded because it targets only a single server, not all servers.
func hasK3sArgForServers(k3dConfig *v1alpha5.SimpleConfig, flag string) bool {
	for _, arg := range k3dConfig.Options.K3sOptions.ExtraArgs {
		if arg.Arg != flag {
			continue
		}

		if len(arg.NodeFilters) == 0 {
			return true
		}

		if slices.Contains(arg.NodeFilters, k3dAllServersNodeFilter) {
			return true
		}
	}

	return false
}

func setupK3dMetricsServer(clusterCfg *v1alpha1.Cluster, k3dConfig *v1alpha5.SimpleConfig) {
	maybeDisableK3dFeature(
		clusterCfg, k3dConfig,
		clusterCfg.Spec.Cluster.MetricsServer == v1alpha1.MetricsServerDisabled,
		k3sDisableMetricsServerFlag,
	)
}

// setupK3dCSI configures K3d to disable local-storage when CSI is explicitly disabled.
func setupK3dCSI(clusterCfg *v1alpha1.Cluster, k3dConfig *v1alpha5.SimpleConfig) {
	maybeDisableK3dFeature(
		clusterCfg, k3dConfig,
		clusterCfg.Spec.Cluster.CSI == v1alpha1.CSIDisabled,
		k3sDisableLocalStorageFlag,
	)
}

// setupK3dLoadBalancer configures K3d to disable servicelb when LoadBalancer is explicitly disabled.
func setupK3dLoadBalancer(clusterCfg *v1alpha1.Cluster, k3dConfig *v1alpha5.SimpleConfig) {
	maybeDisableK3dFeature(
		clusterCfg, k3dConfig,
		clusterCfg.Spec.Cluster.LoadBalancer == v1alpha1.LoadBalancerDisabled,
		k3sDisableServiceLBFlag,
	)
}

// setupVClusterCNI configures the vCluster to disable flannel when a non-default
// CNI (Cilium or Calico) is selected. Without this, the vCluster starts flannel,
// causing conflicts when the custom CNI is installed post-creation.
func setupVClusterCNI(
	clusterCfg *v1alpha1.Cluster,
	vclusterConfig *clusterprovisioner.VClusterConfig,
) {
	if clusterCfg.Spec.Cluster.Distribution != v1alpha1.DistributionVCluster {
		return
	}

	if vclusterConfig == nil {
		return
	}

	cni := clusterCfg.Spec.Cluster.CNI
	if cni != v1alpha1.CNICilium && cni != v1alpha1.CNICalico {
		return
	}

	vclusterConfig.DisableFlannel = true
}

// applyClusterNameOverride updates distribution configs with the cluster name override.
// This function mutates the distribution config pointers in ctx to apply the --name flag value.
// The name override takes highest priority over distribution config or context-derived names.
//
// For Talos, this regenerates the config bundle with the new cluster name because
// the cluster name is embedded in PKI certificates and the kubeconfig context name.
func applyClusterNameOverride(ctx *localregistry.Context, name string) error {
	if name == "" {
		return nil
	}

	applyDirectClusterNameOverrides(ctx, name)

	// Update Talos config - must regenerate bundle for new cluster name
	// because cluster name is embedded in PKI and kubeconfig context
	if ctx.TalosConfig != nil {
		newConfig, err := ctx.TalosConfig.WithName(name)
		if err != nil {
			return fmt.Errorf("failed to apply cluster name override to Talos config: %w", err)
		}

		ctx.TalosConfig = newConfig
	}

	// Update the ksail.yaml context to match the pattern the created cluster uses.
	// Must be provider-aware: the Kubernetes (k3k) provider writes a "k3k-<name>"
	// context for K3s rather than the standalone "k3d-<name>", so post-creation CNI
	// install can resolve it. eksctl adds the creating AWS identity to EKS context
	// names, so that context is resolved from the written kubeconfig after creation.
	if ctx.ClusterCfg != nil &&
		ctx.ClusterCfg.Spec.Cluster.Distribution != v1alpha1.DistributionEKS {
		ctx.ClusterCfg.Spec.Cluster.Connection.Context = resolveCreatedContextName(
			ctx.ClusterCfg.Spec.Cluster.Distribution,
			ctx.ClusterCfg.Spec.Cluster.Provider,
			name,
		)
	}

	return nil
}

// applyDirectClusterNameOverrides updates in-memory distribution configs whose names directly drive
// creation. Talos is handled separately because renaming it must regenerate its PKI-bearing bundle.
// EKS is deliberately excluded: eksctl creates from the unchanged on-disk eks.yaml, so changing only
// EKSConfig.Name would make later state and deletion target a cluster that was never created.
func applyDirectClusterNameOverrides(ctx *localregistry.Context, name string) {
	if ctx.KindConfig != nil {
		ctx.KindConfig.Name = name
	}

	if ctx.K3dConfig != nil {
		ctx.K3dConfig.Name = name
	}

	if ctx.VClusterConfig != nil {
		ctx.VClusterConfig.Name = name
	}

	if ctx.KWOKConfig != nil {
		ctx.KWOKConfig.Name = name
	}
}

// importCachedImages imports container images from a tar archive to the cluster.
// This is called after cluster creation but before component installation to ensure
// CNI, CSI, metrics-server, and other components can use pre-loaded images.
func importCachedImages(
	cmd *cobra.Command,
	ctx *localregistry.Context,
	importPath string,
	tmr timer.Timer,
) error {
	outputTimer := flags.MaybeTimer(cmd, tmr)

	notify.WriteMessage(notify.Message{
		Type:    notify.ActivityType,
		Content: "importing cached images from %s",
		Args:    []any{importPath},
		Writer:  cmd.OutOrStdout(),
	})

	// Use the existing image import functionality
	importer, cleanup, err := imagesvc.NewImporterFromDefaultClient()
	if err != nil {
		return err //nolint:wrapcheck // NewImporterFromDefaultClient already labels the error
	}

	defer cleanup()

	// Resolve cluster name from distribution configs
	clusterName := resolveClusterNameFromContext(ctx)

	err = importer.Import(
		cmd.Context(),
		clusterName,
		ctx.ClusterCfg.Spec.Cluster.Distribution,
		ctx.ClusterCfg.Spec.Cluster.Provider,
		imagesvc.ImportOptions{
			InputPath: importPath,
		},
	)
	if err != nil {
		return fmt.Errorf("import images: %w", err)
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.SuccessType,
		Content: "images imported successfully",
		Timer:   outputTimer,
		Writer:  cmd.OutOrStdout(),
	})

	return nil
}

// resolveClusterNameFromContext determines the cluster name from distribution configs.
func resolveClusterNameFromContext(ctx *localregistry.Context) string {
	switch ctx.ClusterCfg.Spec.Cluster.Distribution {
	case v1alpha1.DistributionVanilla:
		return kindconfigmanager.ResolveClusterName(ctx.ClusterCfg, ctx.KindConfig)
	case v1alpha1.DistributionK3s:
		return k3dconfigmanager.ResolveClusterName(ctx.ClusterCfg, ctx.K3dConfig)
	case v1alpha1.DistributionTalos:
		return talosconfigmanager.ResolveClusterName(ctx.ClusterCfg, ctx.TalosConfig)
	case v1alpha1.DistributionVCluster:
		return resolveVClusterName(ctx)
	case v1alpha1.DistributionKWOK:
		return resolveKWOKName(ctx)
	case v1alpha1.DistributionEKS:
		return resolveEKSName(ctx)
	case v1alpha1.DistributionGKE, v1alpha1.DistributionAKS:
		// GKE/AKS configs are owned by their cloud tooling and not cached on the local registry
		// context; fall back to the cluster-level name.
		return resolveFallbackName(ctx)
	default:
		return resolveFallbackName(ctx)
	}
}

func resolveEKSName(ctx *localregistry.Context) string {
	if ctx.EKSConfig != nil && strings.TrimSpace(ctx.EKSConfig.Name) != "" {
		return strings.TrimSpace(ctx.EKSConfig.Name)
	}

	return resolveFallbackName(ctx)
}

func resolveVClusterName(ctx *localregistry.Context) string {
	if ctx.VClusterConfig != nil && ctx.VClusterConfig.Name != "" {
		return ctx.VClusterConfig.Name
	}

	return "vcluster-default"
}

func resolveKWOKName(ctx *localregistry.Context) string {
	if ctx.KWOKConfig != nil && ctx.KWOKConfig.Name != "" {
		return ctx.KWOKConfig.Name
	}

	return "kwok-default"
}

func resolveFallbackName(ctx *localregistry.Context) string {
	// Connection context takes priority because --name flag updates it via applyClusterNameOverride
	if name := strings.TrimSpace(ctx.ClusterCfg.Spec.Cluster.Connection.Context); name != "" {
		return name
	}

	// Fall back to metadata.name from ksail.yaml
	if name := strings.TrimSpace(ctx.ClusterCfg.Name); name != "" {
		return name
	}

	return "ksail"
}

// maybeWaitForTTL parses the --ttl flag and, if set, blocks to auto-destroy the cluster
// after the TTL duration expires. TTL state is persisted for display in
// `ksail cluster list` and `ksail cluster info`, and the function then blocks by
// calling waitForTTLAndDelete until the cluster is removed or an error occurs.
func maybeWaitForTTL(
	cmd *cobra.Command,
	clusterName string,
	clusterCfg *v1alpha1.Cluster,
	eksConfig *clusterprovisioner.EKSConfig,
) error {
	ttlStr, _ := cmd.Flags().GetString("ttl")
	if ttlStr == "" {
		return nil
	}

	ttl, err := time.ParseDuration(ttlStr)
	if err != nil {
		notify.Warningf(cmd.OutOrStdout(),
			"invalid --ttl value %q: %v (cluster created without TTL)", ttlStr, err)

		return nil
	}

	if ttl <= 0 {
		return nil
	}

	clusterName, err = ttlAutoDeleteTargetName(clusterName, clusterCfg, eksConfig)
	if err != nil {
		return fmt.Errorf("resolve TTL auto-delete target: %w", err)
	}

	// Persist TTL for informational display (ksail cluster list / info).
	saveErr := state.SaveClusterTTL(clusterName, ttl)
	if saveErr != nil {
		notify.Warningf(cmd.OutOrStdout(),
			"failed to save cluster TTL: %v", saveErr)
	}

	// Block and wait for TTL, then auto-destroy.
	return waitForTTLAndDelete(cmd, clusterName, clusterCfg, eksConfig, ttl)
}
