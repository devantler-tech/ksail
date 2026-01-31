package mirrorregistry

import (
	"context"
	"errors"
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

// ErrNoRegistriesFound is returned when no registries are found on the network.
var ErrNoRegistriesFound = errors.New("no registries found on network")

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

// DiscoveredRegistries holds registry information discovered before cluster deletion.
// This is used when the network will be destroyed during cluster deletion (e.g., Talos).
type DiscoveredRegistries struct {
	Registries []dockerclient.RegistryInfo
}

// DiscoverRegistries finds all registries connected to the cluster network.
// This should be called BEFORE cluster deletion for distributions that destroy
// the network during deletion (e.g., Talos).
func DiscoverRegistries(
	cmd *cobra.Command,
	clusterCfg *v1alpha1.Cluster,
	clusterName string,
	cleanupDeps CleanupDependencies,
) *DiscoveredRegistries {
	return DiscoverRegistriesByNetwork(
		cmd,
		clusterCfg.Spec.Cluster.Distribution,
		clusterName,
		cleanupDeps,
	)
}

// DiscoverRegistriesByNetwork finds all registries connected to the cluster network.
// This is a simplified version that doesn't require a cluster config object.
func DiscoverRegistriesByNetwork(
	cmd *cobra.Command,
	distribution v1alpha1.Distribution,
	clusterName string,
	cleanupDeps CleanupDependencies,
) *DiscoveredRegistries {
	networkName := getNetworkNameForDistribution(distribution, clusterName)

	var registries []dockerclient.RegistryInfo

	_ = cleanupDeps.DockerInvoker(cmd, func(dockerClient client.APIClient) error {
		registryMgr, mgrErr := dockerclient.NewRegistryManager(dockerClient)
		if mgrErr != nil {
			return fmt.Errorf("create registry manager: %w", mgrErr)
		}

		discovered, listErr := registryMgr.ListRegistriesOnNetwork(cmd.Context(), networkName)
		if listErr != nil {
			return fmt.Errorf("list registries on network: %w", listErr)
		}

		registries = discovered

		return nil
	})

	return &DiscoveredRegistries{Registries: registries}
}

// deleteRegistriesByInfoCore performs the core deletion of registries by their info.
// Returns the list of deleted registry names.
func deleteRegistriesByInfoCore(
	cmd *cobra.Command,
	registries []dockerclient.RegistryInfo,
	deleteVolumes bool,
	cleanupDeps CleanupDependencies,
) ([]string, error) {
	var deletedNames []string

	err := cleanupDeps.DockerInvoker(cmd, func(dockerClient client.APIClient) error {
		registryMgr, mgrErr := dockerclient.NewRegistryManager(dockerClient)
		if mgrErr != nil {
			return fmt.Errorf("failed to create registry manager: %w", mgrErr)
		}

		names, deleteErr := registryMgr.DeleteRegistriesByInfo(
			cmd.Context(),
			registries,
			deleteVolumes,
		)
		if deleteErr != nil {
			return fmt.Errorf("failed to delete registries: %w", deleteErr)
		}

		deletedNames = names

		return nil
	})

	return deletedNames, err
}

// CleanupPreDiscoveredRegistries deletes registries that were discovered before cluster deletion.
// This is the exported version for use by the simplified delete command.
func CleanupPreDiscoveredRegistries(
	cmd *cobra.Command,
	tmr timer.Timer,
	registries []dockerclient.RegistryInfo,
	deleteVolumes bool,
	cleanupDeps CleanupDependencies,
) error {
	if len(registries) == 0 {
		return ErrNoRegistriesFound
	}

	deletedNames, err := deleteRegistriesByInfoCore(cmd, registries, deleteVolumes, cleanupDeps)
	if err != nil {
		return err
	}

	displayRegistryCleanupOutputWithTimer(cmd, tmr, deletedNames)

	return nil
}

// deleteRegistriesOnNetworkCore performs the core deletion of registries on a network.
// Returns the list of deleted registry names and true if registries were found.
func deleteRegistriesOnNetworkCore(
	cmd *cobra.Command,
	networkName string,
	deleteVolumes bool,
	cleanupDeps CleanupDependencies,
) ([]string, bool, error) {
	var registryNames []string

	found := false

	err := cleanupDeps.DockerInvoker(cmd, func(dockerClient client.APIClient) error {
		registryMgr, mgrErr := dockerclient.NewRegistryManager(dockerClient)
		if mgrErr != nil {
			return fmt.Errorf("failed to create registry manager: %w", mgrErr)
		}

		// Discover registries connected to the network
		registries, listErr := registryMgr.ListRegistriesOnNetwork(cmd.Context(), networkName)
		if listErr != nil {
			return fmt.Errorf("failed to list registries on network: %w", listErr)
		}

		if len(registries) == 0 {
			return nil
		}

		found = true

		// Extract names for notification
		for _, reg := range registries {
			registryNames = append(registryNames, reg.Name)
		}

		// Delete all registries on the network
		_, deleteErr := registryMgr.DeleteRegistriesOnNetwork(
			cmd.Context(),
			networkName,
			deleteVolumes,
		)
		if deleteErr != nil {
			return fmt.Errorf("failed to delete registries: %w", deleteErr)
		}

		return nil
	})

	return registryNames, found, err
}

// CleanupRegistriesByNetwork discovers and cleans up all registry containers by network.
// This is the exported version for use by the simplified delete command.
func CleanupRegistriesByNetwork(
	cmd *cobra.Command,
	tmr timer.Timer,
	distribution v1alpha1.Distribution,
	clusterName string,
	deleteVolumes bool,
	cleanupDeps CleanupDependencies,
) error {
	networkName := getNetworkNameForDistribution(distribution, clusterName)

	registryNames, found, err := deleteRegistriesOnNetworkCore(
		cmd,
		networkName,
		deleteVolumes,
		cleanupDeps,
	)
	if err != nil {
		return err
	}

	if !found {
		return ErrNoRegistriesFound
	}

	displayRegistryCleanupOutputWithTimer(cmd, tmr, registryNames)

	return nil
}

// DisconnectRegistriesFromNetwork disconnects all registries from a network.
// This is used for Talos which needs registries disconnected BEFORE cluster deletion.
func DisconnectRegistriesFromNetwork(
	cmd *cobra.Command,
	networkName string,
	cleanupDeps CleanupDependencies,
) error {
	return cleanupDeps.DockerInvoker(cmd, func(dockerClient client.APIClient) error {
		registryMgr, mgrErr := dockerclient.NewRegistryManager(dockerClient)
		if mgrErr != nil {
			return fmt.Errorf("failed to create registry manager: %w", mgrErr)
		}

		_, disconnectErr := registryMgr.DisconnectAllFromNetwork(cmd.Context(), networkName)
		if disconnectErr != nil {
			return fmt.Errorf(
				"failed to disconnect registries from network %s: %w",
				networkName,
				disconnectErr,
			)
		}

		return nil
	})
}

// displayRegistryCleanupOutputWithTimer shows the cleanup stage output for deleted registries.
// This version uses timer.Timer directly instead of lifecycle.Deps.
func displayRegistryCleanupOutputWithTimer(
	cmd *cobra.Command,
	tmr timer.Timer,
	deletedNames []string,
) {
	if len(deletedNames) == 0 {
		return
	}

	if tmr != nil {
		tmr.NewStage()
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.TitleType,
		Content: "Delete registries...",
		Emoji:   "🗑️",
		Writer:  cmd.OutOrStdout(),
	})

	for _, name := range deletedNames {
		notify.WriteMessage(notify.Message{
			Type:    notify.ActivityType,
			Content: "deleting '%s'",
			Writer:  cmd.OutOrStdout(),
			Args:    []any{name},
		})
	}

	outputTimer := helpers.MaybeTimer(cmd, tmr)

	notify.WriteMessage(notify.Message{
		Type:    notify.SuccessType,
		Content: "registries deleted",
		Timer:   outputTimer,
		Writer:  cmd.OutOrStdout(),
	})
}

// CleanupAll cleans up all registries (both mirror and local) during cluster deletion.
// If preDiscovered is provided, it uses that list instead of discovering registries.
// This is necessary for distributions like Talos where the network is destroyed
// during cluster deletion.
func CleanupAll(
	cmd *cobra.Command,
	_ *ksailconfigmanager.ConfigManager,
	clusterCfg *v1alpha1.Cluster,
	deps lifecycle.Deps,
	clusterName string,
	deleteVolumes bool,
	cleanupDeps CleanupDependencies,
	preDiscovered *DiscoveredRegistries,
) {
	var err error

	if preDiscovered != nil && len(preDiscovered.Registries) > 0 {
		// Use pre-discovered registries (for Talos where network is destroyed)
		err = cleanupPreDiscoveredRegistries(
			cmd,
			deps,
			preDiscovered.Registries,
			deleteVolumes,
			cleanupDeps,
		)
	} else {
		// Discover and cleanup registries by network (for Kind, K3d)
		networkName := getNetworkNameForDistribution(
			clusterCfg.Spec.Cluster.Distribution,
			clusterName,
		)
		err = cleanupRegistriesByNetwork(
			cmd,
			deps,
			networkName,
			deleteVolumes,
			cleanupDeps,
		)
	}

	if err != nil {
		notify.WriteMessage(notify.Message{
			Type:    notify.ErrorType,
			Content: fmt.Sprintf("failed to cleanup registries: %v", err),
			Writer:  cmd.OutOrStdout(),
		})
	}
}

// cleanupPreDiscoveredRegistries deletes registries that were discovered before cluster deletion.
func cleanupPreDiscoveredRegistries(
	cmd *cobra.Command,
	deps lifecycle.Deps,
	registries []dockerclient.RegistryInfo,
	deleteVolumes bool,
	cleanupDeps CleanupDependencies,
) error {
	if len(registries) == 0 {
		return nil
	}

	deletedNames, err := deleteRegistriesByInfoCore(cmd, registries, deleteVolumes, cleanupDeps)
	if err != nil {
		return err
	}

	displayRegistryCleanupOutputWithTimer(cmd, deps.Timer, deletedNames)

	return nil
}

// getNetworkNameForDistribution returns the Docker network name for a given distribution.
func getNetworkNameForDistribution(distribution v1alpha1.Distribution, clusterName string) string {
	switch distribution {
	case v1alpha1.DistributionVanilla:
		return "kind"
	case v1alpha1.DistributionK3s:
		return "k3d-" + clusterName
	case v1alpha1.DistributionTalos:
		return clusterName
	default:
		return clusterName
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
	case v1alpha1.DistributionVanilla:
		return cleanupKindMirrorRegistries(
			cmd,
			cfgManager,
			clusterCfg,
			deps,
			clusterName,
			deleteVolumes,
			cleanupDeps,
		)
	case v1alpha1.DistributionK3s:
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
	cmd *cobra.Command,
	cfgManager *ksailconfigmanager.ConfigManager,
	mirrorsDir string,
) ([]registry.MirrorSpec, []string, error) {
	// Get mirror registry specs with defaults applied
	mirrors := GetMirrorRegistriesWithDefaults(cmd, cfgManager)
	flagSpecs := registry.ParseMirrorSpecs(mirrors)

	// Try to read existing hosts.toml files.
	existingSpecs, err := registry.ReadExistingHostsToml(mirrorsDir)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read existing hosts configuration: %w", err)
	}

	// Merge specs: flag specs override existing specs
	mirrorSpecs := registry.MergeSpecs(existingSpecs, flagSpecs)

	specs, names := buildMirrorSpecsResult(mirrorSpecs)

	return specs, names, nil
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
		cmd,
		cfgManager,
		GetKindMirrorsDir(clusterCfg),
	)
	if err != nil {
		return err
	}

	// Kind uses "kind" as the network name
	networkName := "kind"

	// If no registry specs found from config (non-scaffolded cluster),
	// fall back to network-based discovery
	if len(registryNames) == 0 {
		return cleanupRegistriesByNetwork(
			cmd,
			deps,
			networkName,
			deleteVolumes,
			cleanupDeps,
		)
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
	// K3d uses "k3d-{clusterName}" as the network name
	networkName := "k3d-" + clusterName

	// Use cached distribution config from ConfigManager
	k3dConfig := cfgManager.DistributionConfig.K3d
	if k3dConfig == nil {
		// No config found (non-scaffolded cluster), fall back to network-based discovery
		return cleanupRegistriesByNetwork(
			cmd,
			deps,
			networkName,
			deleteVolumes,
			cleanupDeps,
		)
	}

	registriesInfo := k3dprovisioner.ExtractRegistriesFromConfig(k3dConfig, clusterName)

	registryNames := registry.CollectRegistryNames(registriesInfo)
	if len(registryNames) == 0 {
		// No registries in config, fall back to network-based discovery
		return cleanupRegistriesByNetwork(
			cmd,
			deps,
			networkName,
			deleteVolumes,
			cleanupDeps,
		)
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
	mirrorSpecs, registryNames := CollectTalosMirrorSpecs(cmd, cfgManager)

	// Talos uses the cluster name as the network name
	networkName := clusterName

	// If no registry specs found from config (non-scaffolded cluster),
	// fall back to network-based discovery
	if len(registryNames) == 0 {
		return cleanupRegistriesByNetwork(
			cmd,
			deps,
			networkName,
			deleteVolumes,
			cleanupDeps,
		)
	}

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

// cleanupRegistriesByNetwork discovers and cleans up all registry containers
// (both local and mirror registries) by inspecting the Docker network.
// This unified approach works for both scaffolded and non-scaffolded clusters.
func cleanupRegistriesByNetwork(
	cmd *cobra.Command,
	deps lifecycle.Deps,
	networkName string,
	deleteVolumes bool,
	cleanupDeps CleanupDependencies,
) error {
	registryNames, _, err := deleteRegistriesOnNetworkCore(
		cmd,
		networkName,
		deleteVolumes,
		cleanupDeps,
	)
	if err != nil {
		return err
	}

	displayRegistryCleanupOutputWithTimer(cmd, deps.Timer, registryNames)

	return nil
}

// CollectTalosMirrorSpecs collects mirror specs from Talos config and command line flags.
// This extracts mirror hosts from the loaded Talos config bundle which includes any
// mirror-registries.yaml patches that were applied during cluster creation.
func CollectTalosMirrorSpecs(
	cmd *cobra.Command,
	cfgManager *ksailconfigmanager.ConfigManager,
) ([]registry.MirrorSpec, []string) {
	// Get mirror registry specs with defaults applied
	mirrors := GetMirrorRegistriesWithDefaults(cmd, cfgManager)
	flagSpecs := registry.ParseMirrorSpecs(mirrors)

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

	return buildMirrorSpecsResult(mirrorSpecs)
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
	mirrorSpecs, registryNames := CollectTalosMirrorSpecs(cmd, cfgManager)

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
			Type:    notify.ErrorType,
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
			Type:    notify.ErrorType,
			Content: fmt.Sprintf("failed to disconnect local registry: %v", err),
			Writer:  cmd.OutOrStdout(),
		})
	}
}

// buildMirrorSpecsResult builds the registry names from mirror specs.
// This is a shared helper used by CollectMirrorSpecs and CollectTalosMirrorSpecs.
func buildMirrorSpecsResult(
	mirrorSpecs []registry.MirrorSpec,
) ([]registry.MirrorSpec, []string) {
	if len(mirrorSpecs) == 0 {
		return nil, nil
	}

	// Build registry info to get container names
	entries := registry.BuildMirrorEntries(mirrorSpecs, "", nil, nil, nil)

	registryNames := make([]string, 0, len(entries))
	for _, entry := range entries {
		registryNames = append(registryNames, entry.ContainerName)
	}

	return mirrorSpecs, registryNames
}
