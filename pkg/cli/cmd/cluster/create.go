package cluster

import (
	"context"
	"fmt"
	"strings"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v5/pkg/cli/helpers"
	"github.com/devantler-tech/ksail/v5/pkg/cli/lifecycle"
	"github.com/devantler-tech/ksail/v5/pkg/cli/setup"
	"github.com/devantler-tech/ksail/v5/pkg/cli/setup/localregistry"
	"github.com/devantler-tech/ksail/v5/pkg/cli/setup/mirrorregistry"
	"github.com/devantler-tech/ksail/v5/pkg/client/docker"
	runtime "github.com/devantler-tech/ksail/v5/pkg/di"
	configmanager "github.com/devantler-tech/ksail/v5/pkg/io/config-manager"
	k3dconfigmanager "github.com/devantler-tech/ksail/v5/pkg/io/config-manager/k3d"
	kindconfigmanager "github.com/devantler-tech/ksail/v5/pkg/io/config-manager/kind"
	ksailconfigmanager "github.com/devantler-tech/ksail/v5/pkg/io/config-manager/ksail"
	talosconfigmanager "github.com/devantler-tech/ksail/v5/pkg/io/config-manager/talos"
	imagesvc "github.com/devantler-tech/ksail/v5/pkg/svc/image"
	clusterprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster"
	"github.com/devantler-tech/ksail/v5/pkg/utils/notify"
	"github.com/devantler-tech/ksail/v5/pkg/utils/timer"
	"github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	"github.com/spf13/cobra"
)

const (
	k3sDisableMetricsServerFlag = "--disable=metrics-server"
	k3sDisableLocalStorageFlag  = "--disable=local-storage"
	k3sDisableServiceLBFlag     = "--disable=servicelb"
)

// newCreateLifecycleConfig creates the lifecycle configuration for cluster creation.
func newCreateLifecycleConfig() lifecycle.Config {
	return lifecycle.Config{
		TitleEmoji:         "🚀",
		TitleContent:       "Create cluster...",
		ActivityContent:    "creating cluster",
		SuccessContent:     "cluster created",
		ErrorMessagePrefix: "failed to create cluster",
		Action: func(ctx context.Context, provisioner clusterprovisioner.ClusterProvisioner, clusterName string) error {
			return provisioner.Create(ctx, clusterName)
		},
	}
}

// NewCreateCmd wires the cluster create command using the shared runtime container.
func NewCreateCmd(runtimeContainer *runtime.Runtime) *cobra.Command {
	cmd := &cobra.Command{
		Use:          "create",
		Short:        "Create a cluster",
		Long:         `Create a Kubernetes cluster as defined by configuration.`,
		SilenceUsage: true,
		Annotations: map[string]string{
			annotations.AnnotationPermission: "write",
		},
	}

	fieldSelectors := ksailconfigmanager.DefaultClusterFieldSelectors()
	fieldSelectors = append(fieldSelectors, ksailconfigmanager.DefaultProviderFieldSelector())
	fieldSelectors = append(fieldSelectors, ksailconfigmanager.DefaultCNIFieldSelector())
	fieldSelectors = append(fieldSelectors, ksailconfigmanager.DefaultMetricsServerFieldSelector())
	fieldSelectors = append(fieldSelectors, ksailconfigmanager.DefaultLoadBalancerFieldSelector())
	fieldSelectors = append(fieldSelectors, ksailconfigmanager.DefaultCertManagerFieldSelector())
	fieldSelectors = append(fieldSelectors, ksailconfigmanager.DefaultPolicyEngineFieldSelector())
	fieldSelectors = append(fieldSelectors, ksailconfigmanager.DefaultCSIFieldSelector())
	fieldSelectors = append(fieldSelectors, ksailconfigmanager.DefaultImportImagesFieldSelector())
	fieldSelectors = append(fieldSelectors, ksailconfigmanager.ControlPlanesFieldSelector())
	fieldSelectors = append(fieldSelectors, ksailconfigmanager.WorkersFieldSelector())

	cfgManager := ksailconfigmanager.NewCommandConfigManager(cmd, fieldSelectors)

	cmd.Flags().StringSlice("mirror-registry", []string{},
		"Configure mirror registries with optional authentication. Format: [user:pass@]host[=upstream]. "+
			"Credentials support environment variables using ${VAR} syntax. "+
			"Examples: docker.io=https://registry-1.docker.io, ${USER}:${TOKEN}@ghcr.io=https://ghcr.io")

	// NOTE: mirror-registry is NOT bound to Viper to allow custom merge logic
	// It's handled manually via getMirrorRegistriesWithDefaults() in setup/mirrorregistry

	cmd.Flags().StringP("name", "n", "",
		"Cluster name used for container names, registry names, and kubeconfig context")
	_ = cfgManager.Viper.BindPFlag("name", cmd.Flags().Lookup("name"))

	cmd.RunE = lifecycle.WrapHandler(runtimeContainer, cfgManager, handleCreateRunE)

	return cmd
}

// handleCreateRunE executes cluster creation with mirror registry setup and CNI installation.
//
//nolint:funlen // Orchestrates full cluster creation lifecycle with multiple stages.
func handleCreateRunE(
	cmd *cobra.Command,
	cfgManager *ksailconfigmanager.ConfigManager,
	deps lifecycle.Deps,
) error {
	deps.Timer.Start()

	outputTimer := helpers.MaybeTimer(cmd, deps.Timer)

	ctx, err := loadClusterConfiguration(cfgManager, outputTimer)
	if err != nil {
		return err
	}

	// Apply cluster name override from --name flag if provided
	nameOverride := cfgManager.Viper.GetString("name")
	if nameOverride != "" {
		// Validate cluster name is DNS-1123 compliant
		validationErr := v1alpha1.ValidateClusterName(nameOverride)
		if validationErr != nil {
			return fmt.Errorf("invalid --name flag: %w", validationErr)
		}

		err = applyClusterNameOverride(ctx, nameOverride)
		if err != nil {
			return err
		}
	}

	// Early validation of distribution x provider combination
	err = ctx.ClusterCfg.Spec.Cluster.Provider.ValidateForDistribution(
		ctx.ClusterCfg.Spec.Cluster.Distribution,
	)
	if err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	localDeps := getLocalRegistryDeps()

	err = ensureLocalRegistriesReady(
		cmd,
		ctx,
		deps,
		cfgManager,
		localDeps,
	)
	if err != nil {
		return err
	}

	setupK3dMetricsServer(ctx.ClusterCfg, ctx.K3dConfig)
	SetupK3dCSI(ctx.ClusterCfg, ctx.K3dConfig)
	SetupK3dLoadBalancer(ctx.ClusterCfg, ctx.K3dConfig)

	configureClusterProvisionerFactory(&deps, ctx)

	err = executeClusterLifecycle(cmd, ctx.ClusterCfg, deps)
	if err != nil {
		return err
	}

	configureRegistryMirrorsInClusterWithWarning(
		cmd,
		ctx,
		deps,
		cfgManager,
	)

	err = localregistry.ExecuteStage(
		cmd,
		ctx,
		deps,
		localregistry.StageConnect,
		localDeps,
	)
	if err != nil {
		return fmt.Errorf("failed to connect local registry: %w", err)
	}

	// Wait for K3d local registry to be ready before installing components.
	// K3d creates the registry during cluster creation, so we need to wait
	// for it to be ready before Flux can sync from it.
	err = localregistry.WaitForK3dLocalRegistryReady(
		cmd,
		ctx.ClusterCfg,
		ctx.K3dConfig,
		localDeps.DockerInvoker,
	)
	if err != nil {
		return fmt.Errorf("failed to wait for local registry: %w", err)
	}

	maybeImportCachedImages(cmd, ctx, deps.Timer)

	return handlePostCreationSetup(cmd, ctx.ClusterCfg, deps.Timer)
}

// configureClusterProvisionerFactory sets up the cluster provisioner factory.
// Uses test override if available, otherwise creates a default factory.
func configureClusterProvisionerFactory(
	deps *lifecycle.Deps,
	ctx *localregistry.Context,
) {
	clusterProvisionerFactoryMu.RLock()

	factoryOverride := clusterProvisionerFactoryOverride

	clusterProvisionerFactoryMu.RUnlock()

	if factoryOverride != nil {
		deps.Factory = factoryOverride
	} else {
		deps.Factory = clusterprovisioner.DefaultFactory{
			DistributionConfig: &clusterprovisioner.DistributionConfig{
				Kind:  ctx.KindConfig,
				K3d:   ctx.K3dConfig,
				Talos: ctx.TalosConfig,
			},
		}
	}
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

	// Image import is not supported for Talos clusters
	if ctx.ClusterCfg.Spec.Cluster.Distribution == v1alpha1.DistributionTalos {
		notify.WriteMessage(notify.Message{
			Type:    notify.WarningType,
			Content: "image import is not supported for Talos clusters; ignoring --import-images value %q",
			Args:    []any{importPath},
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
) mirrorregistry.StageParams {
	localDeps := getLocalRegistryDeps()

	return mirrorregistry.StageParams{
		Cmd:           cmd,
		ClusterCfg:    ctx.ClusterCfg,
		Deps:          deps,
		CfgManager:    cfgManager,
		KindConfig:    ctx.KindConfig,
		K3dConfig:     ctx.K3dConfig,
		TalosConfig:   ctx.TalosConfig,
		DockerInvoker: localDeps.DockerInvoker,
	}
}

func ensureLocalRegistriesReady(
	cmd *cobra.Command,
	ctx *localregistry.Context,
	deps lifecycle.Deps,
	cfgManager *ksailconfigmanager.ConfigManager,
	localDeps localregistry.Dependencies,
) error {
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

	// Stage 2: Verify registry access (for external registries with auth)
	// This gives early feedback if credentials are missing or invalid
	err = localregistry.VerifyRegistryAccess(cmd, ctx.ClusterCfg, deps)
	if err != nil {
		return fmt.Errorf("failed to verify registry access: %w", err)
	}

	params := buildRegistryStageParams(cmd, ctx, deps, cfgManager)

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
) {
	params := buildRegistryStageParams(cmd, ctx, deps, cfgManager)

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
	_, err := setup.InstallCNI(cmd, clusterCfg, tmr)
	if err != nil {
		return fmt.Errorf("failed to install CNI: %w", err)
	}

	factories := getInstallerFactories()
	outputTimer := helpers.MaybeTimer(cmd, tmr)

	// OCI artifact push is now handled inside InstallPostCNIComponents after Flux is installed
	err = setup.InstallPostCNIComponents(
		cmd,
		clusterCfg,
		factories,
		outputTimer,
	)
	if err != nil {
		return fmt.Errorf("failed to install post-CNI components: %w", err)
	}

	return nil
}

func setupK3dMetricsServer(clusterCfg *v1alpha1.Cluster, k3dConfig *v1alpha5.SimpleConfig) {
	if clusterCfg.Spec.Cluster.Distribution != v1alpha1.DistributionK3s || k3dConfig == nil {
		return
	}

	if clusterCfg.Spec.Cluster.MetricsServer != v1alpha1.MetricsServerDisabled {
		return
	}

	for _, arg := range k3dConfig.Options.K3sOptions.ExtraArgs {
		if arg.Arg == k3sDisableMetricsServerFlag {
			return
		}
	}

	k3dConfig.Options.K3sOptions.ExtraArgs = append(
		k3dConfig.Options.K3sOptions.ExtraArgs,
		v1alpha5.K3sArgWithNodeFilters{
			Arg:         k3sDisableMetricsServerFlag,
			NodeFilters: []string{"server:*"},
		},
	)
}

// SetupK3dCSI configures K3d to disable local-storage when CSI is explicitly disabled.
// This function is exported for testing purposes.
func SetupK3dCSI(clusterCfg *v1alpha1.Cluster, k3dConfig *v1alpha5.SimpleConfig) {
	if clusterCfg.Spec.Cluster.Distribution != v1alpha1.DistributionK3s || k3dConfig == nil {
		return
	}

	if clusterCfg.Spec.Cluster.CSI != v1alpha1.CSIDisabled {
		return
	}

	for _, arg := range k3dConfig.Options.K3sOptions.ExtraArgs {
		if arg.Arg == k3sDisableLocalStorageFlag {
			return
		}
	}

	k3dConfig.Options.K3sOptions.ExtraArgs = append(
		k3dConfig.Options.K3sOptions.ExtraArgs,
		v1alpha5.K3sArgWithNodeFilters{
			Arg:         k3sDisableLocalStorageFlag,
			NodeFilters: []string{"server:*"},
		},
	)
}

// SetupK3dLoadBalancer configures K3d to disable servicelb when LoadBalancer is explicitly disabled.
// This function is exported for testing purposes.
func SetupK3dLoadBalancer(clusterCfg *v1alpha1.Cluster, k3dConfig *v1alpha5.SimpleConfig) {
	if clusterCfg.Spec.Cluster.Distribution != v1alpha1.DistributionK3s || k3dConfig == nil {
		return
	}

	if clusterCfg.Spec.Cluster.LoadBalancer != v1alpha1.LoadBalancerDisabled {
		return
	}

	for _, arg := range k3dConfig.Options.K3sOptions.ExtraArgs {
		if arg.Arg == k3sDisableServiceLBFlag {
			return
		}
	}

	k3dConfig.Options.K3sOptions.ExtraArgs = append(
		k3dConfig.Options.K3sOptions.ExtraArgs,
		v1alpha5.K3sArgWithNodeFilters{
			Arg:         k3sDisableServiceLBFlag,
			NodeFilters: []string{"server:*"},
		},
	)
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

	// Update Kind config
	if ctx.KindConfig != nil {
		ctx.KindConfig.Name = name
	}

	// Update K3d config
	if ctx.K3dConfig != nil {
		ctx.K3dConfig.Name = name
	}

	// Update Talos config - must regenerate bundle for new cluster name
	// because cluster name is embedded in PKI and kubeconfig context
	if ctx.TalosConfig != nil {
		newConfig, err := ctx.TalosConfig.WithName(name)
		if err != nil {
			return fmt.Errorf("failed to apply cluster name override to Talos config: %w", err)
		}

		ctx.TalosConfig = newConfig
	}

	// Update the ksail.yaml context to match the distribution pattern
	if ctx.ClusterCfg != nil {
		dist := ctx.ClusterCfg.Spec.Cluster.Distribution
		ctx.ClusterCfg.Spec.Cluster.Connection.Context = dist.ContextName(name)
	}

	return nil
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
	outputTimer := helpers.MaybeTimer(cmd, tmr)

	notify.WriteMessage(notify.Message{
		Type:    notify.ActivityType,
		Emoji:   "📥",
		Content: "importing cached images from %s",
		Args:    []any{importPath},
		Timer:   outputTimer,
		Writer:  cmd.OutOrStdout(),
	})

	// Use the existing image import functionality
	dockerClient, err := docker.GetDockerClient()
	if err != nil {
		return fmt.Errorf("failed to create Docker client: %w", err)
	}

	defer func() { _ = dockerClient.Close() }()

	importer := imagesvc.NewImporter(dockerClient)

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
	default:
		// Fallback to context name or default
		if name := strings.TrimSpace(ctx.ClusterCfg.Spec.Cluster.Connection.Context); name != "" {
			return name
		}

		return "ksail"
	}
}
