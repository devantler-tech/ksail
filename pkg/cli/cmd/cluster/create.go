package cluster

import (
	"context"
	"fmt"
	"sync"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/cli/cmd/cluster/components"
	"github.com/devantler-tech/ksail/v5/pkg/cli/create"
	"github.com/devantler-tech/ksail/v5/pkg/cli/create/registrystage"
	"github.com/devantler-tech/ksail/v5/pkg/cli/docker"
	"github.com/devantler-tech/ksail/v5/pkg/cli/flags"
	"github.com/devantler-tech/ksail/v5/pkg/cli/lifecycle"
	"github.com/devantler-tech/ksail/v5/pkg/cli/ui/notify"
	"github.com/devantler-tech/ksail/v5/pkg/cli/ui/timer"
	runtime "github.com/devantler-tech/ksail/v5/pkg/di"
	ksailconfigmanager "github.com/devantler-tech/ksail/v5/pkg/io/config-manager/ksail"
	"github.com/devantler-tech/ksail/v5/pkg/svc/installer"
	clusterprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster"
	"github.com/docker/docker/client"
	"github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	"github.com/spf13/cobra"
)

const (
	k3sDisableMetricsServerFlag = "--disable=metrics-server"
)

// registryStageInfo contains display information for a registry stage.
// Used by both local registry and mirror registry stages.
type registryStageInfo struct {
	title         string
	emoji         string
	activity      string
	success       string
	failurePrefix string
}

// Test injection for installer factories, docker client invoker, and cluster provisioner factory.
var (
	//nolint:gochecknoglobals // dependency injection for tests
	installerFactoriesOverrideMu sync.RWMutex
	//nolint:gochecknoglobals // dependency injection for tests
	installerFactoriesOverride *create.InstallerFactories
	//nolint:gochecknoglobals // dependency injection for tests
	dockerClientInvokerMu sync.RWMutex
	//nolint:gochecknoglobals // dependency injection for tests
	clusterProvisionerFactoryMu sync.RWMutex
	//nolint:gochecknoglobals // dependency injection for tests
	clusterProvisionerFactoryOverride clusterprovisioner.Factory
	//nolint:gochecknoglobals // dependency injection for tests
	dockerClientInvoker = docker.WithClient
)

// getInstallerFactories returns the installer factories to use, allowing test override.
func getInstallerFactories() *create.InstallerFactories {
	installerFactoriesOverrideMu.RLock()
	defer installerFactoriesOverrideMu.RUnlock()

	if installerFactoriesOverride != nil {
		return installerFactoriesOverride
	}

	return create.DefaultInstallerFactories()
}

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

	outputTimer := flags.MaybeTimer(cmd, deps.Timer)

	ctx, err := loadClusterConfiguration(cfgManager, outputTimer)
	if err != nil {
		return err
	}

	firstActivityShown := false

	err = ensureLocalRegistriesReady(
		cmd,
		ctx,
		deps,
		cfgManager,
		&firstActivityShown,
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

	connectMirrorRegistriesWithWarning(
		cmd,
		ctx,
		deps,
		cfgManager,
		&firstActivityShown,
	)

	err = executeLocalRegistryStage(
		cmd,
		ctx,
		deps,
		localRegistryStageConnect,
		&firstActivityShown,
	)
	if err != nil {
		return fmt.Errorf("failed to connect local registry: %w", err)
	}

	return handlePostCreationSetup(cmd, ctx.ClusterCfg, deps.Timer, &firstActivityShown)
}

func loadClusterConfiguration(
	cfgManager *ksailconfigmanager.ConfigManager,
	tmr timer.Timer,
) (*ClusterCommandContext, error) {
	_, err := cfgManager.LoadConfig(tmr)
	if err != nil {
		return nil, fmt.Errorf("failed to load cluster configuration: %w", err)
	}

	return NewClusterCommandContext(cfgManager), nil
}

func ensureLocalRegistriesReady(
	cmd *cobra.Command,
	ctx *ClusterCommandContext,
	deps lifecycle.Deps,
	cfgManager *ksailconfigmanager.ConfigManager,
	firstActivityShown *bool,
) error {
	err := executeLocalRegistryStage(
		cmd,
		ctx,
		deps,
		localRegistryStageProvision,
		firstActivityShown,
	)
	if err != nil {
		return fmt.Errorf("failed to provision local registry: %w", err)
	}

	dockerClientInvokerMu.RLock()

	invoker := dockerClientInvoker

	dockerClientInvokerMu.RUnlock()

	err = registrystage.SetupMirrorRegistries(registrystage.StageParams{
		Cmd:                cmd,
		ClusterCfg:         ctx.ClusterCfg,
		Deps:               deps,
		CfgManager:         cfgManager,
		KindConfig:         ctx.KindConfig,
		K3dConfig:          ctx.K3dConfig,
		TalosConfig:        ctx.TalosConfig,
		FirstActivityShown: firstActivityShown,
		DockerInvoker:      invoker,
	})
	if err != nil {
		return fmt.Errorf("failed to setup mirror registries: %w", err)
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

func connectMirrorRegistriesWithWarning(
	cmd *cobra.Command,
	ctx *ClusterCommandContext,
	deps lifecycle.Deps,
	cfgManager *ksailconfigmanager.ConfigManager,
	firstActivityShown *bool,
) {
	dockerClientInvokerMu.RLock()

	invoker := dockerClientInvoker

	dockerClientInvokerMu.RUnlock()

	err := registrystage.ConnectRegistriesToClusterNetwork(registrystage.StageParams{
		Cmd:                cmd,
		ClusterCfg:         ctx.ClusterCfg,
		Deps:               deps,
		CfgManager:         cfgManager,
		KindConfig:         ctx.KindConfig,
		K3dConfig:          ctx.K3dConfig,
		TalosConfig:        ctx.TalosConfig,
		FirstActivityShown: firstActivityShown,
		DockerInvoker:      invoker,
	})
	if err != nil {
		notify.WriteMessage(notify.Message{
			Type:    notify.WarningType,
			Content: fmt.Sprintf("failed to connect registries to cluster network: %v", err),
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
	_, err := components.InstallCNI(cmd, clusterCfg, tmr, firstActivityShown)
	if err != nil {
		return fmt.Errorf("failed to install CNI: %w", err)
	}

	factories := getInstallerFactories()
	outputTimer := flags.MaybeTimer(cmd, tmr)

	err = components.InstallPostCNIComponents(cmd, clusterCfg, factories, outputTimer, firstActivityShown)
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

// Test injection functions

// overrideInstallerFactory is a helper that applies a factory override and returns a restore function.
func overrideInstallerFactory(apply func(*create.InstallerFactories)) func() {
	installerFactoriesOverrideMu.Lock()

	previous := installerFactoriesOverride
	override := create.DefaultInstallerFactories()

	if previous != nil {
		*override = *previous
	}

	apply(override)
	installerFactoriesOverride = override

	installerFactoriesOverrideMu.Unlock()

	return func() {
		installerFactoriesOverrideMu.Lock()
		installerFactoriesOverride = previous
		installerFactoriesOverrideMu.Unlock()
	}
}

// SetCertManagerInstallerFactoryForTests overrides the cert-manager installer factory.
func SetCertManagerInstallerFactoryForTests(
	factory func(*v1alpha1.Cluster) (installer.Installer, error),
) func() {
	return overrideInstallerFactory(func(f *create.InstallerFactories) {
		f.CertManager = factory
	})
}

// SetCSIInstallerFactoryForTests overrides the CSI installer factory.
func SetCSIInstallerFactoryForTests(
	factory func(*v1alpha1.Cluster) (installer.Installer, error),
) func() {
	return overrideInstallerFactory(func(f *create.InstallerFactories) {
		f.CSI = factory
	})
}

// SetArgoCDInstallerFactoryForTests overrides the Argo CD installer factory.
func SetArgoCDInstallerFactoryForTests(
	factory func(*v1alpha1.Cluster) (installer.Installer, error),
) func() {
	return overrideInstallerFactory(func(f *create.InstallerFactories) {
		f.ArgoCD = factory
	})
}

// SetEnsureArgoCDResourcesForTests overrides the Argo CD resource ensure function.
func SetEnsureArgoCDResourcesForTests(
	fn func(context.Context, string, *v1alpha1.Cluster) error,
) func() {
	return overrideInstallerFactory(func(f *create.InstallerFactories) {
		f.EnsureArgoCDResources = fn
	})
}

// SetDockerClientInvokerForTests overrides the Docker client invoker for testing.
func SetDockerClientInvokerForTests(
	invoker func(*cobra.Command, func(client.APIClient) error) error,
) func() {
	dockerClientInvokerMu.Lock()

	previous := dockerClientInvoker
	dockerClientInvoker = invoker

	dockerClientInvokerMu.Unlock()

	return func() {
		dockerClientInvokerMu.Lock()

		dockerClientInvoker = previous

		dockerClientInvokerMu.Unlock()
	}
}

// SetClusterProvisionerFactoryForTests overrides the cluster provisioner factory for testing.
func SetClusterProvisionerFactoryForTests(factory clusterprovisioner.Factory) func() {
	clusterProvisionerFactoryMu.Lock()

	previous := clusterProvisionerFactoryOverride
	clusterProvisionerFactoryOverride = factory

	clusterProvisionerFactoryMu.Unlock()

	return func() {
		clusterProvisionerFactoryMu.Lock()

		clusterProvisionerFactoryOverride = previous

		clusterProvisionerFactoryMu.Unlock()
	}
}

// runRegistryStage executes a registry stage with proper lifecycle management.
// Used by local registry and mirror registry stages.
func runRegistryStage(
	cmd *cobra.Command,
	deps lifecycle.Deps,
	info registryStageInfo,
	action func(context.Context, client.APIClient) error,
	firstActivityShown *bool,
) error {
	deps.Timer.NewStage()

	if *firstActivityShown {
		cmd.Println()
	}

	*firstActivityShown = true

	notify.WriteMessage(notify.Message{
		Type:    notify.TitleType,
		Content: info.title,
		Emoji:   info.emoji,
		Writer:  cmd.OutOrStdout(),
	})

	if info.activity != "" {
		notify.WriteMessage(notify.Message{
			Type:    notify.ActivityType,
			Content: info.activity,
			Writer:  cmd.OutOrStdout(),
		})
	}

	dockerClientInvokerMu.RLock()

	invoker := dockerClientInvoker

	dockerClientInvokerMu.RUnlock()

	err := invoker(cmd, func(dockerClient client.APIClient) error {
		err := action(cmd.Context(), dockerClient)
		if err != nil {
			return fmt.Errorf("%s: %w", info.failurePrefix, err)
		}

		outputTimer := flags.MaybeTimer(cmd, deps.Timer)

		notify.WriteMessage(notify.Message{
			Type:    notify.SuccessType,
			Content: info.success,
			Timer:   outputTimer,
			Writer:  cmd.OutOrStdout(),
		})

		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to execute registry stage: %w", err)
	}

	return nil
}
