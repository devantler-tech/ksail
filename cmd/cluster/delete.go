package cluster

import (
	"context"
	"fmt"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	dockerclient "github.com/devantler-tech/ksail/v5/pkg/client/docker"
	cmdhelpers "github.com/devantler-tech/ksail/v5/pkg/cmd"
	runtime "github.com/devantler-tech/ksail/v5/pkg/di"
	k3dconfigmanager "github.com/devantler-tech/ksail/v5/pkg/io/config-manager/k3d"
	ksailconfigmanager "github.com/devantler-tech/ksail/v5/pkg/io/config-manager/ksail"
	"github.com/devantler-tech/ksail/v5/pkg/io/scaffolder"
	clusterprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster"
	k3dprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/k3d"
	kindprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/kind"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/registry"
	"github.com/devantler-tech/ksail/v5/pkg/ui/notify"
	"github.com/devantler-tech/ksail/v5/pkg/ui/timer"
	"github.com/docker/docker/client"
	"github.com/spf13/cobra"
)

// newDeleteLifecycleConfig creates the lifecycle configuration for cluster deletion.
func newDeleteLifecycleConfig() cmdhelpers.LifecycleConfig {
	return cmdhelpers.LifecycleConfig{
		TitleEmoji:         "🗑️",
		TitleContent:       "Delete cluster...",
		ActivityContent:    "deleting cluster",
		SuccessContent:     "cluster deleted",
		ErrorMessagePrefix: "failed to delete cluster",
		Action: func(ctx context.Context, provisioner clusterprovisioner.ClusterProvisioner, clusterName string) error {
			return provisioner.Delete(ctx, clusterName)
		},
	}
}

// NewDeleteCmd creates and returns the delete command.
func NewDeleteCmd(runtimeContainer *runtime.Runtime) *cobra.Command {
	cmd := &cobra.Command{
		Use:          "delete",
		Short:        "Destroy a cluster",
		Long:         `Destroy a cluster.`,
		SilenceUsage: true,
	}

	cfgManager := ksailconfigmanager.NewCommandConfigManager(
		cmd,
		ksailconfigmanager.DefaultClusterFieldSelectors(),
	)

	// Add flag for controlling registry volume deletion
	cmd.Flags().
		Bool("delete-volumes", false, "Delete registry volumes when cleaning up registries")

	cmd.RunE = cmdhelpers.WrapLifecycleHandler(runtimeContainer, cfgManager, handleDeleteRunE)

	return cmd
}

// handleDeleteRunE executes cluster deletion with registry cleanup.
func handleDeleteRunE(
	cmd *cobra.Command,
	cfgManager *ksailconfigmanager.ConfigManager,
	deps cmdhelpers.LifecycleDeps,
) error {
	clusterCfg := cfgManager.Config

	// Get cluster name respecting the --context flag
	clusterName, err := cmdhelpers.GetClusterNameFromConfig(clusterCfg, deps.Factory)
	if err != nil {
		return fmt.Errorf("failed to get cluster name: %w", err)
	}

	deleteVolumes, flagErr := cmd.Flags().GetBool("delete-volumes")
	if flagErr != nil {
		return fmt.Errorf("failed to get delete-volumes flag: %w", flagErr)
	}

	// For TalosInDocker, we must cleanup registries BEFORE deleting the cluster
	// because the registries are connected to the cluster network. If we delete
	// the cluster first, the network removal fails with "has active endpoints".
	if clusterCfg.Spec.Cluster.Distribution == v1alpha1.DistributionTalosInDocker {
		cleanupRegistriesBeforeDelete(cmd, cfgManager, clusterCfg, deps, clusterName, deleteVolumes)
	}

	config := newDeleteLifecycleConfig()

	// Execute cluster deletion
	err = cmdhelpers.HandleLifecycleRunE(cmd, cfgManager, deps, config)
	if err != nil {
		return fmt.Errorf("cluster deletion failed: %w", err)
	}

	// For non-TalosInDocker distributions, cleanup registries after cluster deletion
	if clusterCfg.Spec.Cluster.Distribution != v1alpha1.DistributionTalosInDocker {
		cleanupRegistriesAfterDelete(cmd, cfgManager, clusterCfg, deps, clusterName, deleteVolumes)
	}

	return nil
}

// cleanupRegistriesBeforeDelete cleans up registries before cluster deletion.
// This is required for TalosInDocker where registries are connected to the cluster network.
func cleanupRegistriesBeforeDelete(
	cmd *cobra.Command,
	cfgManager *ksailconfigmanager.ConfigManager,
	clusterCfg *v1alpha1.Cluster,
	deps cmdhelpers.LifecycleDeps,
	clusterName string,
	deleteVolumes bool,
) {
	err := cleanupMirrorRegistries(cmd, cfgManager, clusterCfg, deps, clusterName, deleteVolumes)
	if err != nil {
		notify.WriteMessage(notify.Message{
			Type:    notify.WarningType,
			Content: fmt.Sprintf("failed to cleanup registries: %v", err),
			Writer:  cmd.OutOrStdout(),
		})
	}

	if clusterCfg.Spec.Cluster.LocalRegistry == v1alpha1.LocalRegistryEnabled {
		err = cleanupLocalRegistry(cmd, clusterCfg, deps, deleteVolumes)
		if err != nil {
			notify.WriteMessage(notify.Message{
				Type:    notify.WarningType,
				Content: fmt.Sprintf("failed to cleanup local registry: %v", err),
				Writer:  cmd.OutOrStdout(),
			})
		}
	}
}

// cleanupRegistriesAfterDelete cleans up registries after cluster deletion.
// This is the default behavior for Kind and K3d distributions.
func cleanupRegistriesAfterDelete(
	cmd *cobra.Command,
	cfgManager *ksailconfigmanager.ConfigManager,
	clusterCfg *v1alpha1.Cluster,
	deps cmdhelpers.LifecycleDeps,
	clusterName string,
	deleteVolumes bool,
) {
	err := cleanupMirrorRegistries(cmd, cfgManager, clusterCfg, deps, clusterName, deleteVolumes)
	if err != nil {
		notify.WriteMessage(notify.Message{
			Type:    notify.WarningType,
			Content: fmt.Sprintf("failed to cleanup registries: %v", err),
			Writer:  cmd.OutOrStdout(),
		})
	}

	if clusterCfg.Spec.Cluster.LocalRegistry == v1alpha1.LocalRegistryEnabled {
		err = cleanupLocalRegistry(cmd, clusterCfg, deps, deleteVolumes)
		if err != nil {
			notify.WriteMessage(notify.Message{
				Type:    notify.WarningType,
				Content: fmt.Sprintf("failed to cleanup local registry: %v", err),
				Writer:  cmd.OutOrStdout(),
			})
		}
	}
}

// cleanupMirrorRegistries cleans up registries for Kind after cluster deletion.
// K3d handles registry cleanup natively through its own configuration.
func cleanupMirrorRegistries(
	cmd *cobra.Command,
	cfgManager *ksailconfigmanager.ConfigManager,
	clusterCfg *v1alpha1.Cluster,
	deps cmdhelpers.LifecycleDeps,
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
		return cleanupK3dMirrorRegistries(cmd, clusterCfg, deps, clusterName, deleteVolumes)
	case v1alpha1.DistributionTalosInDocker:
		return cleanupTalosInDockerMirrorRegistries(
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

	if len(mirrorSpecs) == 0 {
		return nil, nil, nil
	}

	// Build registry info to get names
	entries := registry.BuildMirrorEntries(mirrorSpecs, "", nil, nil, nil)

	registryNames := make([]string, 0, len(entries))
	for _, entry := range entries {
		registryNames = append(registryNames, entry.ContainerName)
	}

	return mirrorSpecs, registryNames, nil
}

func cleanupKindMirrorRegistries(
	cmd *cobra.Command,
	cfgManager *ksailconfigmanager.ConfigManager,
	_ *v1alpha1.Cluster,
	deps cmdhelpers.LifecycleDeps,
	clusterName string,
	deleteVolumes bool,
) error {
	mirrorSpecs, registryNames, err := collectMirrorSpecs(cfgManager, scaffolder.KindMirrorsDir)
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
	)
}

func cleanupK3dMirrorRegistries(
	cmd *cobra.Command,
	clusterCfg *v1alpha1.Cluster,
	deps cmdhelpers.LifecycleDeps,
	clusterName string,
	deleteVolumes bool,
) error {
	if clusterCfg.Spec.Cluster.DistributionConfig == "" {
		return nil
	}

	k3dConfigMgr := k3dconfigmanager.NewConfigManager(clusterCfg.Spec.Cluster.DistributionConfig)

	k3dConfig, loadErr := k3dConfigMgr.LoadConfig(deps.Timer)
	if loadErr != nil {
		return fmt.Errorf("failed to load k3d config: %w", loadErr)
	}

	registriesInfo := k3dprovisioner.ExtractRegistriesFromConfigForTesting(k3dConfig)

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
	)
}

func runMirrorRegistryCleanup(
	cmd *cobra.Command,
	deps cmdhelpers.LifecycleDeps,
	registryNames []string,
	cleanup func(client.APIClient) error,
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

	dockerClientInvokerMu.RLock()

	invoker := dockerClientInvoker

	dockerClientInvokerMu.RUnlock()

	err := invoker(cmd, func(dockerClient client.APIClient) error {
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

	outputTimer := cmdhelpers.MaybeTimer(cmd, tmr)

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

func cleanupTalosInDockerMirrorRegistries(
	cmd *cobra.Command,
	cfgManager *ksailconfigmanager.ConfigManager,
	deps cmdhelpers.LifecycleDeps,
	clusterName string,
	deleteVolumes bool,
) error {
	mirrorSpecs, registryNames, err := collectMirrorSpecs(cfgManager, scaffolder.KindMirrorsDir)
	if err != nil {
		return err
	}

	if len(registryNames) == 0 {
		return nil
	}

	// TalosInDocker uses the cluster name as the network name
	networkName := clusterName

	return runMirrorRegistryCleanup(
		cmd,
		deps,
		registryNames,
		func(dockerAPIClient client.APIClient) error {
			// Build registry infos from mirror specs
			registryInfos := registry.BuildRegistryInfosFromSpecs(mirrorSpecs, nil, nil)

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
	)
}
