package mirrorregistry

import (
	"context"
	"fmt"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/cli/helpers"
	"github.com/devantler-tech/ksail/v5/pkg/cli/lifecycle"
	"github.com/devantler-tech/ksail/v5/pkg/cli/setup/localregistry"
	dockerclient "github.com/devantler-tech/ksail/v5/pkg/client/docker"
	ksailconfigmanager "github.com/devantler-tech/ksail/v5/pkg/io/config-manager/ksail"
	k3dprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/k3d"
	kindprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/kind"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/registry"
	"github.com/devantler-tech/ksail/v5/pkg/utils/notify"
	"github.com/devantler-tech/ksail/v5/pkg/utils/timer"
	"github.com/docker/docker/client"
	"github.com/spf13/cobra"
)

// CleanupDependencies holds dependencies for mirror registry cleanup operations.
type CleanupDependencies struct {
	DockerInvoker     func(*cobra.Command, func(client.APIClient) error) error
	LocalRegistryDeps localregistry.Dependencies
}

// DefaultCleanupDependencies returns the default cleanup dependencies.
func DefaultCleanupDependencies() CleanupDependencies {
	return CleanupDependencies{
		DockerInvoker:     helpers.WithDockerClient,
		LocalRegistryDeps: localregistry.DefaultDependencies(),
	}
}

// CleanupAll cleans up mirror and local registries during cluster deletion.
// For Talos, registries are disconnected from the network before cluster deletion
// (via DisconnectMirrorRegistriesWithWarning and DisconnectLocalRegistryWithWarning),
// but the actual container cleanup happens here after deletion.
func CleanupAll(
	cmd *cobra.Command,
	cfgManager *ksailconfigmanager.ConfigManager,
	clusterCfg *v1alpha1.Cluster,
	deps lifecycle.Deps,
	clusterName string,
	deleteVolumes bool,
	cleanupDeps CleanupDependencies,
) {
	err := CleanupMirrorRegistries(
		cmd,
		cfgManager,
		clusterCfg,
		deps,
		clusterName,
		deleteVolumes,
		cleanupDeps,
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
	err = localregistry.Cleanup(
		cmd,
		cfgManager,
		clusterCfg,
		deps,
		deleteVolumes,
		cleanupDeps.LocalRegistryDeps,
	)
	if err != nil {
		notify.WriteMessage(notify.Message{
			Type:    notify.ErrorType,
			Content: fmt.Sprintf("failed to cleanup local registry: %v", err),
			Writer:  cmd.OutOrStdout(),
		})
	}
}

// CleanupMirrorRegistries cleans up registries for Kind after cluster deletion.
// K3d handles registry cleanup natively through its own configuration.
func CleanupMirrorRegistries(
	cmd *cobra.Command,
	cfgManager *ksailconfigmanager.ConfigManager,
	clusterCfg *v1alpha1.Cluster,
	deps lifecycle.Deps,
	clusterName string,
	deleteVolumes bool,
	cleanupDeps CleanupDependencies,
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
			cleanupDeps,
		)
	case v1alpha1.DistributionK3d:
		return cleanupK3dMirrorRegistries(
			cmd,
			cfgManager,
			deps,
			clusterName,
			deleteVolumes,
			cleanupDeps,
		)
	case v1alpha1.DistributionTalos:
		return cleanupTalosMirrorRegistries(
			cmd,
			cfgManager,
			deps,
			clusterName,
			deleteVolumes,
			cleanupDeps,
		)
	default:
		return nil
	}
}

// CollectMirrorSpecs collects and merges mirror specs from flags and existing config.
// Returns the merged specs, registry names, and any error.
func CollectMirrorSpecs(
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
	cleanupDeps CleanupDependencies,
) error {
	mirrorSpecs, registryNames, err := CollectMirrorSpecs(
		cfgManager,
		GetKindMirrorsDir(clusterCfg),
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
		cleanupDeps,
	)
}

func cleanupK3dMirrorRegistries(
	cmd *cobra.Command,
	cfgManager *ksailconfigmanager.ConfigManager,
	deps lifecycle.Deps,
	clusterName string,
	deleteVolumes bool,
	cleanupDeps CleanupDependencies,
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
		cleanupDeps,
	)
}

func runMirrorRegistryCleanup(
	cmd *cobra.Command,
	deps lifecycle.Deps,
	registryNames []string,
	cleanup func(client.APIClient) error,
	cleanupDeps CleanupDependencies,
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

	err := cleanupDeps.DockerInvoker(cmd, func(dockerClient client.APIClient) error {
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
	cleanupDeps CleanupDependencies,
) error {
	// Collect mirror specs from Talos config (not kind/mirrors directory)
	mirrorSpecs, registryNames := CollectTalosMirrorSpecs(cfgManager)

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
		cleanupDeps,
	)
}

// CollectTalosMirrorSpecs collects mirror specs from Talos config and command line flags.
// This extracts mirror hosts from the loaded Talos config bundle which includes any
// mirror-registries.yaml patches that were applied during cluster creation.
func CollectTalosMirrorSpecs(
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

// DisconnectMirrorRegistries disconnects mirror registries from the Talos network.
// This allows the network to be removed during cluster deletion without "active endpoints" errors.
func DisconnectMirrorRegistries(
	cmd *cobra.Command,
	cfgManager *ksailconfigmanager.ConfigManager,
	clusterName string,
	cleanupDeps CleanupDependencies,
) error {
	// Collect mirror specs from Talos config
	mirrorSpecs, registryNames := CollectTalosMirrorSpecs(cfgManager)

	// Talos uses the cluster name as the network name
	networkName := clusterName

	err := cleanupDeps.DockerInvoker(cmd, func(dockerAPIClient client.APIClient) error {
		registryMgr, mgrErr := dockerclient.NewRegistryManager(dockerAPIClient)
		if mgrErr != nil {
			return fmt.Errorf("failed to create registry manager: %w", mgrErr)
		}

		// If no registry names found from config (non-scaffolded cluster),
		// fall back to discovering and disconnecting all registries from the network
		if len(registryNames) == 0 {
			_, disconnectErr := registryMgr.DisconnectAllFromNetwork(cmd.Context(), networkName)
			if disconnectErr != nil {
				return fmt.Errorf(
					"failed to disconnect registries from network %s: %w",
					networkName,
					disconnectErr,
				)
			}

			return nil
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

// DisconnectMirrorRegistriesWithWarning disconnects mirror registries from the network.
// This is used for Talos which needs registries disconnected BEFORE cluster deletion
// due to network dependencies, while actual container cleanup happens after deletion.
func DisconnectMirrorRegistriesWithWarning(
	cmd *cobra.Command,
	cfgManager *ksailconfigmanager.ConfigManager,
	clusterName string,
	cleanupDeps CleanupDependencies,
) {
	err := DisconnectMirrorRegistries(cmd, cfgManager, clusterName, cleanupDeps)
	if err != nil {
		notify.WriteMessage(notify.Message{
			Type:    notify.WarningType,
			Content: fmt.Sprintf("failed to disconnect mirror registries: %v", err),
			Writer:  cmd.OutOrStdout(),
		})
	}
}

// DisconnectLocalRegistryWithWarning disconnects the local registry from the cluster network.
// This is used for Talos which needs registries disconnected BEFORE cluster deletion
// because the registry is connected to the cluster network.
func DisconnectLocalRegistryWithWarning(
	cmd *cobra.Command,
	cfgManager *ksailconfigmanager.ConfigManager,
	clusterCfg *v1alpha1.Cluster,
	deps lifecycle.Deps,
	clusterName string,
	cleanupDeps CleanupDependencies,
) {
	err := localregistry.Disconnect(
		cmd,
		cfgManager,
		clusterCfg,
		deps,
		clusterName,
		cleanupDeps.LocalRegistryDeps,
	)
	if err != nil {
		notify.WriteMessage(notify.Message{
			Type:    notify.WarningType,
			Content: fmt.Sprintf("failed to disconnect local registry: %v", err),
			Writer:  cmd.OutOrStdout(),
		})
	}
}

// buildMirrorSpecsResult builds the registry names from mirror specs.
// This is a shared helper used by CollectMirrorSpecs.
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
