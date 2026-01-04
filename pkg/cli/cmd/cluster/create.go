package cluster

import (
	"context"
	"fmt"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/cli/helpers"
	"github.com/devantler-tech/ksail/v5/pkg/cli/lifecycle"
	"github.com/devantler-tech/ksail/v5/pkg/cli/setup"
	"github.com/devantler-tech/ksail/v5/pkg/cli/setup/localregistry"
	"github.com/devantler-tech/ksail/v5/pkg/cli/setup/mirrorregistry"
	runtime "github.com/devantler-tech/ksail/v5/pkg/di"
	ksailconfigmanager "github.com/devantler-tech/ksail/v5/pkg/io/config-manager/ksail"
	clusterprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster"
	"github.com/devantler-tech/ksail/v5/pkg/utils/notify"
	"github.com/devantler-tech/ksail/v5/pkg/utils/timer"
	"github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	"github.com/spf13/cobra"
)

const (
	k3sDisableMetricsServerFlag = "--disable=metrics-server"
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
	}

	fieldSelectors := ksailconfigmanager.DefaultClusterFieldSelectors()
	fieldSelectors = append(fieldSelectors, ksailconfigmanager.DefaultCNIFieldSelector())
	fieldSelectors = append(fieldSelectors, ksailconfigmanager.DefaultMetricsServerFieldSelector())
	fieldSelectors = append(fieldSelectors, ksailconfigmanager.DefaultCertManagerFieldSelector())
	fieldSelectors = append(fieldSelectors, ksailconfigmanager.DefaultCSIFieldSelector())
	fieldSelectors = append(fieldSelectors, ksailconfigmanager.ControlPlanesFieldSelector())
	fieldSelectors = append(fieldSelectors, ksailconfigmanager.WorkersFieldSelector())

	cfgManager := ksailconfigmanager.NewCommandConfigManager(cmd, fieldSelectors)

	cmd.Flags().StringSlice("mirror-registry", []string{},
		"Configure mirror registries with format 'host=upstream' (e.g., docker.io=https://registry-1.docker.io)")
	_ = cfgManager.Viper.BindPFlag("mirror-registry", cmd.Flags().Lookup("mirror-registry"))

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

	firstActivityShown := false
	localDeps := getLocalRegistryDeps()

	err = ensureLocalRegistriesReady(
		cmd,
		ctx,
		deps,
		cfgManager,
		&firstActivityShown,
		localDeps,
	)
	if err != nil {
		return err
	}

	setupK3dMetricsServer(ctx.ClusterCfg, ctx.K3dConfig)

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

	err = executeClusterLifecycle(cmd, ctx.ClusterCfg, deps, &firstActivityShown)
	if err != nil {
		return err
	}

	configureRegistryMirrorsInClusterWithWarning(
		cmd,
		ctx,
		deps,
		cfgManager,
		&firstActivityShown,
	)

	err = localregistry.ExecuteStage(
		cmd,
		ctx,
		deps,
		localregistry.StageConnect,
		&firstActivityShown,
		localDeps,
	)
	if err != nil {
		return fmt.Errorf("failed to connect local registry: %w", err)
	}

	// Wait for K3d local registry to be ready before installing components.
	// K3d creates the registry during cluster creation, so we need to wait
	// for it to be ready before Flux can sync from it.
	err = localregistry.WaitForK3dLocalRegistryReady(cmd, ctx.ClusterCfg, ctx.K3dConfig, localDeps.DockerInvoker)
	if err != nil {
		return fmt.Errorf("failed to wait for local registry: %w", err)
	}

	return handlePostCreationSetup(cmd, ctx.ClusterCfg, deps.Timer, &firstActivityShown)
}

func loadClusterConfiguration(
	cfgManager *ksailconfigmanager.ConfigManager,
	tmr timer.Timer,
) (*localregistry.Context, error) {
	// Load config to populate cfgManager.Config and cfgManager.DistributionConfig
	// The returned config is cached in cfgManager.Config, which is used by NewContextFromConfigManager
	_, err := cfgManager.LoadConfig(tmr)
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
	firstActivityShown *bool,
) mirrorregistry.StageParams {
	localDeps := getLocalRegistryDeps()

	return mirrorregistry.StageParams{
		Cmd:                cmd,
		ClusterCfg:         ctx.ClusterCfg,
		Deps:               deps,
		CfgManager:         cfgManager,
		KindConfig:         ctx.KindConfig,
		K3dConfig:          ctx.K3dConfig,
		TalosConfig:        ctx.TalosConfig,
		FirstActivityShown: firstActivityShown,
		DockerInvoker:      localDeps.DockerInvoker,
	}
}

func ensureLocalRegistriesReady(
	cmd *cobra.Command,
	ctx *localregistry.Context,
	deps lifecycle.Deps,
	cfgManager *ksailconfigmanager.ConfigManager,
	firstActivityShown *bool,
	localDeps localregistry.Dependencies,
) error {
	// Stage 1: Provision local registry
	err := localregistry.ExecuteStage(
		cmd,
		ctx,
		deps,
		localregistry.StageProvision,
		firstActivityShown,
		localDeps,
	)
	if err != nil {
		return fmt.Errorf("failed to provision local registry: %w", err)
	}

	params := buildRegistryStageParams(cmd, ctx, deps, cfgManager, firstActivityShown)

	// Stage 2: Create and configure registry containers (local + mirrors)
	err = mirrorregistry.SetupRegistries(params)
	if err != nil {
		return fmt.Errorf("failed to setup registries: %w", err)
	}

	// Stage 3: Create Docker network
	err = mirrorregistry.CreateNetwork(params)
	if err != nil {
		return fmt.Errorf("failed to create docker network: %w", err)
	}

	// Stage 4: Connect registries to network (before cluster creation)
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
	firstActivityShown *bool,
) error {
	deps.Timer.NewStage()

	if *firstActivityShown {
		cmd.Println()
	}

	*firstActivityShown = true

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
	firstActivityShown *bool,
) {
	params := buildRegistryStageParams(cmd, ctx, deps, cfgManager, firstActivityShown)

	// Configure containerd inside cluster nodes to use registry mirrors (Kind only)
	err := mirrorregistry.ConfigureRegistryMirrorsInCluster(params)
	if err != nil {
		notify.WriteMessage(notify.Message{
			Type:    notify.WarningType,
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
	firstActivityShown *bool,
) error {
	_, err := setup.InstallCNI(cmd, clusterCfg, tmr, firstActivityShown)
	if err != nil {
		return fmt.Errorf("failed to install CNI: %w", err)
	}

	factories := getInstallerFactories()
	outputTimer := helpers.MaybeTimer(cmd, tmr)

	err = setup.InstallPostCNIComponents(
		cmd,
		clusterCfg,
		factories,
		outputTimer,
		firstActivityShown,
	)
	if err != nil {
		return fmt.Errorf("failed to install post-CNI components: %w", err)
	}

	return nil
}

func setupK3dMetricsServer(clusterCfg *v1alpha1.Cluster, k3dConfig *v1alpha5.SimpleConfig) {
	if clusterCfg.Spec.Cluster.Distribution != v1alpha1.DistributionK3d || k3dConfig == nil {
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
