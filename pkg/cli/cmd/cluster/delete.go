package cluster

import (
	"context"
	"errors"
	"fmt"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/cli/helpers"
	"github.com/devantler-tech/ksail/v5/pkg/cli/lifecycle"
	"github.com/devantler-tech/ksail/v5/pkg/cli/setup/localregistry"
	dockerclient "github.com/devantler-tech/ksail/v5/pkg/client/docker"
	runtime "github.com/devantler-tech/ksail/v5/pkg/di"
	ksailconfigmanager "github.com/devantler-tech/ksail/v5/pkg/io/config-manager/ksail"
	"github.com/devantler-tech/ksail/v5/pkg/io/scaffolder"
	clusterprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster"
	clustererrors "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/errors"
	k3dprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/k3d"
	kindprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/kind"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/registry"
	"github.com/devantler-tech/ksail/v5/pkg/utils/notify"
	"github.com/devantler-tech/ksail/v5/pkg/utils/timer"
	"github.com/docker/docker/client"
	"github.com/spf13/cobra"
)

// NewDeleteCmd creates and returns the delete command.
func NewDeleteCmd(runtimeContainer *runtime.Runtime) *cobra.Command {
	cmd := &cobra.Command{
		Use:           "delete",
		Short:         "Destroy a cluster",
		Long:          `Destroy a cluster.`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	cfgManager := ksailconfigmanager.NewCommandConfigManager(
		cmd,
		ksailconfigmanager.DefaultClusterFieldSelectors(),
	)

	// Add flag for controlling registry volume deletion
	cmd.Flags().
		Bool("delete-volumes", false, "Delete registry volumes when cleaning up registries")

	cmd.RunE = lifecycle.WrapHandler(runtimeContainer, cfgManager, handleDeleteRunE)

	return cmd
}

// handleDeleteRunE executes cluster deletion with registry cleanup.
func handleDeleteRunE(
	cmd *cobra.Command,
	cfgManager *ksailconfigmanager.ConfigManager,
	deps lifecycle.Deps,
) error {
	clusterCfg := cfgManager.Config
	deps = applyFactoryOverride(deps)

	// If no config file was found, try to detect distribution from kubeconfig context
	if !cfgManager.IsConfigFileFound() {
		detectedCfg, detectedDeps, detectErr := tryContextBasedDetection(cmd, clusterCfg, deps)
		if detectErr == nil {
			clusterCfg = detectedCfg
			deps = detectedDeps
		}
		// If detection fails, fall back to the default config-based approach
	}

	clusterName, err := lifecycle.GetClusterNameFromConfig(clusterCfg, deps.Factory)
	if err != nil {
		return fmt.Errorf("failed to get cluster name: %w", err)
	}

	deleteVolumes, flagErr := cmd.Flags().GetBool("delete-volumes")
	if flagErr != nil {
		return fmt.Errorf("failed to get delete-volumes flag: %w", flagErr)
	}

	if deps.Timer != nil {
		deps.Timer.NewStage()
	}

	err = executeClusterDeletion(cmd, cfgManager, deps, clusterCfg)
	if err != nil {
		return err
	}

	cleanupRegistries(cmd, cfgManager, clusterCfg, deps, clusterName, deleteVolumes)

	return nil
}

// tryContextBasedDetection attempts to detect the distribution and cluster name from the kubeconfig context.
// This is used when no ksail.yaml config file is found, allowing delete to work with non-scaffolded clusters.
func tryContextBasedDetection(
	cmd *cobra.Command,
	clusterCfg *v1alpha1.Cluster,
	deps lifecycle.Deps,
) (*v1alpha1.Cluster, lifecycle.Deps, error) {
	// Get current context from kubeconfig
	currentContext, err := lifecycle.GetCurrentKubeContext()
	if err != nil {
		return nil, deps, fmt.Errorf("failed to get current context: %w", err)
	}

	// Detect distribution and cluster name from context pattern
	distribution, clusterName, err := lifecycle.DetectDistributionFromContext(currentContext)
	if err != nil {
		return nil, deps, fmt.Errorf("failed to detect distribution: %w", err)
	}

	// Update the config with detected values
	clusterCfg.Spec.Cluster.Distribution = distribution
	clusterCfg.Spec.Cluster.Connection.Context = currentContext

	// Create a minimal provisioner for the detected distribution
	provisioner, err := lifecycle.CreateMinimalProvisioner(distribution, clusterName)
	if err != nil {
		return nil, deps, fmt.Errorf("failed to create provisioner: %w", err)
	}

	// Create a factory that returns the minimal provisioner
	deps.Factory = &contextBasedFactory{
		distribution: distribution,
		clusterName:  clusterName,
		provisioner:  provisioner,
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.InfoType,
		Content: fmt.Sprintf("auto-detected %s cluster '%s' from context", distribution, clusterName),
		Writer:  cmd.OutOrStdout(),
	})

	return clusterCfg, deps, nil
}

// contextBasedFactory is a factory that returns a pre-created provisioner for context-based detection.
type contextBasedFactory struct {
	distribution v1alpha1.Distribution
	clusterName  string
	provisioner  clusterprovisioner.ClusterProvisioner
}

// Create returns the pre-created provisioner.
func (f *contextBasedFactory) Create(
	_ context.Context,
	_ *v1alpha1.Cluster,
) (clusterprovisioner.ClusterProvisioner, any, error) {
	return f.provisioner, nil, nil
}

// applyFactoryOverride applies any test factory override to deps.
func applyFactoryOverride(deps lifecycle.Deps) lifecycle.Deps {
	clusterProvisionerFactoryMu.RLock()

	factoryOverride := clusterProvisionerFactoryOverride

	clusterProvisionerFactoryMu.RUnlock()

	if factoryOverride != nil {
		deps.Factory = factoryOverride
	}

	return deps
}

// disconnectTalosRegistriesWithContext disconnects registries from Talos network before deletion.
func disconnectTalosRegistriesWithContext(
	_ context.Context, // ctx is available via cmd.Context() in called functions
	cmd *cobra.Command,
	cfgManager *ksailconfigmanager.ConfigManager,
	clusterCfg *v1alpha1.Cluster,
	deps lifecycle.Deps,
	clusterName string,
) {
	if clusterCfg.Spec.Cluster.Distribution != v1alpha1.DistributionTalos {
		return
	}

	//nolint:contextcheck // Functions use cmd.Context() internally
	disconnectMirrorRegistriesWithWarning(cmd, cfgManager, clusterCfg, deps, clusterName)
	//nolint:contextcheck // Functions use cmd.Context() internally
	disconnectLocalRegistryWithWarning(cmd, cfgManager, clusterCfg, deps, clusterName)
}

// executeClusterDeletion runs the cluster deletion and handles "not found" gracefully.
func executeClusterDeletion(
	cmd *cobra.Command,
	cfgManager *ksailconfigmanager.ConfigManager,
	deps lifecycle.Deps,
	clusterCfg *v1alpha1.Cluster,
) error {
	config := lifecycle.Config{
		TitleEmoji:         "🗑️",
		TitleContent:       "Delete cluster...",
		ActivityContent:    "deleting cluster",
		SuccessContent:     "cluster deleted",
		ErrorMessagePrefix: "failed to delete cluster",
		Action: func(
			ctx context.Context,
			provisioner clusterprovisioner.ClusterProvisioner,
			name string,
		) error {
			// Check if cluster exists first
			exists, err := provisioner.Exists(ctx, name)
			if err != nil {
				return fmt.Errorf("check cluster existence: %w", err)
			}

			if !exists {
				return clustererrors.ErrClusterNotFound
			}

			// Disconnect registries before Talos cluster deletion to avoid network conflicts
			disconnectTalosRegistriesWithContext(ctx, cmd, cfgManager, clusterCfg, deps, name)

			return provisioner.Delete(ctx, name)
		},
	}

err := lifecycle.RunWithConfig(cmd, deps, config, clusterCfg)
	if err != nil {
		if errors.Is(err, clustererrors.ErrClusterNotFound) {
			notify.WriteMessage(notify.Message{
				Type:    notify.ErrorType,
				Content: "cluster does not exist, nothing to delete",
				Timer:   helpers.MaybeTimer(cmd, deps.Timer),
				Writer:  cmd.OutOrStdout(),
			})

			return clustererrors.ErrClusterNotFound
		}

		return fmt.Errorf("cluster deletion failed: %w", err)
	}

	return nil
}

// disconnectMirrorRegistriesWithWarning disconnects mirror registries from the network.
// This is used for Talos which needs registries disconnected BEFORE cluster deletion
// due to network dependencies, while actual container cleanup happens after deletion.
func disconnectMirrorRegistriesWithWarning(
	cmd *cobra.Command,
	cfgManager *ksailconfigmanager.ConfigManager,
	_ *v1alpha1.Cluster,
	_ lifecycle.Deps,
	clusterName string,
) {
	err := disconnectMirrorRegistries(cmd, cfgManager, clusterName)
	if err != nil {
		notify.WriteMessage(notify.Message{
			Type:    notify.WarningType,
			Content: fmt.Sprintf("failed to disconnect mirror registries: %v", err),
			Writer:  cmd.OutOrStdout(),
		})
	}
}

// disconnectLocalRegistryWithWarning disconnects the local registry from the cluster network.
// This is used for Talos which needs registries disconnected BEFORE cluster deletion
// because the registry is connected to the cluster network.
func disconnectLocalRegistryWithWarning(
	cmd *cobra.Command,
	cfgManager *ksailconfigmanager.ConfigManager,
	clusterCfg *v1alpha1.Cluster,
	deps lifecycle.Deps,
	clusterName string,
) {
	localDeps := getLocalRegistryDeps()

	err := localregistry.Disconnect(cmd, cfgManager, clusterCfg, deps, clusterName, localDeps)
	if err != nil {
		notify.WriteMessage(notify.Message{
			Type:    notify.WarningType,
			Content: fmt.Sprintf("failed to disconnect local registry: %v", err),
			Writer:  cmd.OutOrStdout(),
		})
	}
}

// cleanupRegistries cleans up mirror and local registries during cluster deletion.
// For Talos, registries are disconnected from the network before cluster deletion
// (via disconnectMirrorRegistriesWithWarning and disconnectLocalRegistryWithWarning),
// but the actual container cleanup happens here after deletion.
func cleanupRegistries(
	cmd *cobra.Command,
	cfgManager *ksailconfigmanager.ConfigManager,
	clusterCfg *v1alpha1.Cluster,
	deps lifecycle.Deps,
	clusterName string,
	deleteVolumes bool,
) {
	err := cleanupMirrorRegistries(
		cmd,
		cfgManager,
		clusterCfg,
		deps,
		clusterName,
		deleteVolumes,
	)
	if err != nil {
		notify.WriteMessage(notify.Message{
			Type:    notify.WarningType,
			Content: fmt.Sprintf("failed to cleanup registries: %v", err),
			Writer:  cmd.OutOrStdout(),
		})
	}

	// Attempt local registry cleanup for Kind and Talos (K3d handles it natively).
	// The Cleanup function checks for container existence and skips if not provisioned.
	// This ensures orphaned containers are cleaned up even when config is missing.
	localDeps := getLocalRegistryDeps()

	err = localregistry.Cleanup(cmd, cfgManager, clusterCfg, deps, deleteVolumes, localDeps)
	if err != nil {
		notify.WriteMessage(notify.Message{
			Type:    notify.WarningType,
			Content: fmt.Sprintf("failed to cleanup local registry: %v", err),
			Writer:  cmd.OutOrStdout(),
		})
	}
}

// cleanupMirrorRegistries cleans up registries for Kind after cluster deletion.
// K3d handles registry cleanup natively through its own configuration.
func cleanupMirrorRegistries(
	cmd *cobra.Command,
	cfgManager *ksailconfigmanager.ConfigManager,
	clusterCfg *v1alpha1.Cluster,
	deps lifecycle.Deps,
	clusterName string,
	deleteVolumes bool,
) error {
	switch clusterCfg.Spec.Cluster.Distribution {
	case v1alpha1.DistributionKind:
		return cleanupKindMirrorRegistries(
			cmd,
			cfgManager,
			clusterCfg,
			deps,
			clusterName,
			deleteVolumes,
		)
	case v1alpha1.DistributionK3d:
		return cleanupK3dMirrorRegistries(
			cmd,
			cfgManager,
			deps,
			clusterName,
			deleteVolumes,
		)
	case v1alpha1.DistributionTalos:
		return cleanupTalosMirrorRegistries(
			cmd,
			cfgManager,
			deps,
			clusterName,
			deleteVolumes,
		)
	default:
		return nil
	}
}

// collectMirrorSpecs collects and merges mirror specs from flags and existing config.
// Returns the merged specs, registry names, and any error.
func collectMirrorSpecs(
	cfgManager *ksailconfigmanager.ConfigManager,
	mirrorsDir string,
) ([]registry.MirrorSpec, []string, error) {
	// Get mirror registry specs from command line flag
	flagSpecs := registry.ParseMirrorSpecs(cfgManager.Viper.GetStringSlice("mirror-registry"))

	// Try to read existing hosts.toml files.
	existingSpecs, err := registry.ReadExistingHostsToml(mirrorsDir)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read existing hosts configuration: %w", err)
	}

	// Merge specs: flag specs override existing specs
	mirrorSpecs := registry.MergeSpecs(existingSpecs, flagSpecs)

	return buildMirrorSpecsResult(mirrorSpecs)
}

func cleanupKindMirrorRegistries(
	cmd *cobra.Command,
	cfgManager *ksailconfigmanager.ConfigManager,
	clusterCfg *v1alpha1.Cluster,
	deps lifecycle.Deps,
	clusterName string,
	deleteVolumes bool,
) error {
	mirrorSpecs, registryNames, err := collectMirrorSpecs(
		cfgManager,
		getKindMirrorsDirForCluster(clusterCfg),
	)
	if err != nil {
		return err
	}

	if len(registryNames) == 0 {
		return nil
	}

	return runMirrorRegistryCleanup(
		cmd,
		deps,
		registryNames,
		func(dockerClient client.APIClient) error {
			return kindprovisioner.CleanupRegistries(
				cmd.Context(),
				mirrorSpecs,
				clusterName,
				dockerClient,
				deleteVolumes,
			)
		},
		getLocalRegistryDeps(),
	)
}

func cleanupK3dMirrorRegistries(
	cmd *cobra.Command,
	cfgManager *ksailconfigmanager.ConfigManager,
	deps lifecycle.Deps,
	clusterName string,
	deleteVolumes bool,
) error {
	// Use cached distribution config from ConfigManager
	k3dConfig := cfgManager.DistributionConfig.K3d
	if k3dConfig == nil {
		return nil
	}

	registriesInfo := k3dprovisioner.ExtractRegistriesFromConfigForTesting(k3dConfig, clusterName)

	registryNames := registry.CollectRegistryNames(registriesInfo)
	if len(registryNames) == 0 {
		return nil
	}

	return runMirrorRegistryCleanup(
		cmd,
		deps,
		registryNames,
		func(dockerClient client.APIClient) error {
			return k3dprovisioner.CleanupRegistries(
				cmd.Context(),
				k3dConfig,
				clusterName,
				dockerClient,
				deleteVolumes,
				cmd.ErrOrStderr(),
			)
		},
		getLocalRegistryDeps(),
	)
}

func runMirrorRegistryCleanup(
	cmd *cobra.Command,
	deps lifecycle.Deps,
	registryNames []string,
	cleanup func(client.APIClient) error,
	localDeps localregistry.Dependencies,
) error {
	if len(registryNames) == 0 {
		return nil
	}

	deps.Timer.NewStage()

	cmd.Println()
	notify.WriteMessage(notify.Message{
		Type:    notify.TitleType,
		Content: "Delete mirror registry...",
		Emoji:   "🗑️",
		Writer:  cmd.OutOrStdout(),
	})

	err := localDeps.DockerInvoker(cmd, func(dockerClient client.APIClient) error {
		return executeRegistryCleanup(cmd, dockerClient, registryNames, cleanup, deps.Timer)
	})
	if err != nil {
		return fmt.Errorf("failed to delete mirror registries: %w", err)
	}

	return nil
}

func executeRegistryCleanup(
	cmd *cobra.Command,
	dockerClient client.APIClient,
	registryNames []string,
	cleanup func(client.APIClient) error,
	tmr timer.Timer,
) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	registryMgr, _ := dockerclient.NewRegistryManager(dockerClient)

	err := cleanup(dockerClient)
	if err != nil {
		return fmt.Errorf("failed to cleanup registries: %w", err)
	}

	notifyRegistryDeletions(ctx, cmd, registryNames, registryMgr)

	outputTimer := helpers.MaybeTimer(cmd, tmr)

	notify.WriteMessage(notify.Message{
		Type:    notify.SuccessType,
		Content: "mirror registries deleted",
		Timer:   outputTimer,
		Writer:  cmd.OutOrStdout(),
	})

	return nil
}

func notifyRegistryDeletions(
	ctx context.Context,
	cmd *cobra.Command,
	registryNames []string,
	registryMgr *dockerclient.RegistryManager,
) {
	for _, name := range registryNames {
		content := "deleting '%s'"

		if registryMgr != nil {
			inUse, checkErr := registryMgr.IsRegistryInUse(ctx, name)
			if checkErr == nil && inUse {
				content = "skipping '%s' as it is in use"
			}
		}

		notify.WriteMessage(notify.Message{
			Type:    notify.ActivityType,
			Content: content,
			Writer:  cmd.OutOrStdout(),
			Args:    []any{name},
		})
	}
}

func cleanupTalosMirrorRegistries(
	cmd *cobra.Command,
	cfgManager *ksailconfigmanager.ConfigManager,
	deps lifecycle.Deps,
	clusterName string,
	deleteVolumes bool,
) error {
	// Collect mirror specs from Talos config (not kind/mirrors directory)
	mirrorSpecs, registryNames := collectTalosMirrorSpecs(cfgManager)

	if len(registryNames) == 0 {
		return nil
	}

	// Talos uses the cluster name as the network name
	networkName := clusterName

	return runMirrorRegistryCleanup(
		cmd,
		deps,
		registryNames,
		func(dockerAPIClient client.APIClient) error {
			// Build registry infos from mirror specs
			registryInfos := registry.BuildRegistryInfosFromSpecs(
				mirrorSpecs,
				nil,
				nil,
				clusterName,
			)

			if len(registryInfos) == 0 {
				return nil
			}

			// Create registry manager
			registryMgr, mgrErr := dockerclient.NewRegistryManager(dockerAPIClient)
			if mgrErr != nil {
				return fmt.Errorf("failed to create registry manager: %w", mgrErr)
			}

			return registry.CleanupRegistries(
				cmd.Context(),
				registryMgr,
				registryInfos,
				clusterName,
				deleteVolumes,
				networkName,
				nil,
			)
		},
		getLocalRegistryDeps(),
	)
}

// collectTalosMirrorSpecs collects mirror specs from Talos config and command line flags.
// This extracts mirror hosts from the loaded Talos config bundle which includes any
// mirror-registries.yaml patches that were applied during cluster creation.
func collectTalosMirrorSpecs(
	cfgManager *ksailconfigmanager.ConfigManager,
) ([]registry.MirrorSpec, []string) {
	// Get mirror registry specs from command line flag
	flagSpecs := registry.ParseMirrorSpecs(cfgManager.Viper.GetStringSlice("mirror-registry"))

	// Extract mirror hosts from the loaded Talos config
	var talosSpecs []registry.MirrorSpec

	if cfgManager.DistributionConfig != nil && cfgManager.DistributionConfig.Talos != nil {
		talosHosts := cfgManager.DistributionConfig.Talos.ExtractMirrorHosts()
		for _, host := range talosHosts {
			talosSpecs = append(talosSpecs, registry.MirrorSpec{
				Host:   host,
				Remote: registry.GenerateUpstreamURL(host),
			})
		}
	}

	// Merge specs: flag specs override Talos config specs for the same host
	mirrorSpecs := registry.MergeSpecs(talosSpecs, flagSpecs)

	specs, names, _ := buildMirrorSpecsResult(mirrorSpecs)

	return specs, names
}

// disconnectMirrorRegistries disconnects mirror registries from the Talos network.
// This allows the network to be removed during cluster deletion without "active endpoints" errors.
func disconnectMirrorRegistries(
	cmd *cobra.Command,
	cfgManager *ksailconfigmanager.ConfigManager,
	clusterName string,
) error {
	// Collect mirror specs from Talos config
	mirrorSpecs, registryNames := collectTalosMirrorSpecs(cfgManager)

	if len(registryNames) == 0 {
		return nil
	}

	// Talos uses the cluster name as the network name
	networkName := clusterName
	localDeps := getLocalRegistryDeps()

	err := localDeps.DockerInvoker(cmd, func(dockerAPIClient client.APIClient) error {
		registryMgr, mgrErr := dockerclient.NewRegistryManager(dockerAPIClient)
		if mgrErr != nil {
			return fmt.Errorf("failed to create registry manager: %w", mgrErr)
		}

		// Build registry infos from mirror specs to get container names
		registryInfos := registry.BuildRegistryInfosFromSpecs(
			mirrorSpecs,
			nil,
			nil,
			clusterName,
		)

		// Disconnect each registry from the network
		for _, info := range registryInfos {
			disconnectErr := registryMgr.DisconnectFromNetwork(
				cmd.Context(),
				info.Name,
				networkName,
			)
			if disconnectErr != nil {
				return fmt.Errorf(
					"failed to disconnect registry %s from network: %w",
					info.Name,
					disconnectErr,
				)
			}
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("disconnect mirror registries: %w", err)
	}

	return nil
}

// buildMirrorSpecsResult builds the registry names from mirror specs.
// This is a shared helper used by collectMirrorSpecs.
func buildMirrorSpecsResult(
	mirrorSpecs []registry.MirrorSpec,
) ([]registry.MirrorSpec, []string, error) {
	if len(mirrorSpecs) == 0 {
		return nil, nil, nil
	}

	// Build registry info to get container names
	entries := registry.BuildMirrorEntries(mirrorSpecs, "", nil, nil, nil)

	registryNames := make([]string, 0, len(entries))
	for _, entry := range entries {
		registryNames = append(registryNames, entry.ContainerName)
	}

	return mirrorSpecs, registryNames, nil
}

// getKindMirrorsDir returns the configured Kind mirrors directory or the default.
func getKindMirrorsDirForCluster(clusterCfg *v1alpha1.Cluster) string {
	if clusterCfg != nil && clusterCfg.Spec.Cluster.Kind.MirrorsDir != "" {
		return clusterCfg.Spec.Cluster.Kind.MirrorsDir
	}

	return scaffolder.DefaultKindMirrorsDir
}
