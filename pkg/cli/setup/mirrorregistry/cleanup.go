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
	"github.com/devantler-tech/ksail/v5/pkg/notify"
	k3dprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/k3d"
	kindprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/kind"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/registry"
	"github.com/devantler-tech/ksail/v5/pkg/timer"
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
			clusterName,
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

// CleanupRegistriesByNetwork discovers and cleans up all registry containers by network.
// This is the exported version for use by the simplified delete command.
// Only registries belonging to the specified cluster (by name prefix) are deleted.
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
		clusterName,
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
			clusterCfg.Spec.Cluster.Provider,
		)
	default:
		return nil
	}
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

// deleteRegistriesOnNetworkCore performs the core deletion of registries on a network.
// Returns the list of deleted registry names and true if registries were found.
// Only registries belonging to the specified cluster (by name prefix) are deleted.
func deleteRegistriesOnNetworkCore(
	cmd *cobra.Command,
	networkName string,
	clusterName string,
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
		allRegistries, listErr := registryMgr.ListRegistriesOnNetwork(cmd.Context(), networkName)
		if listErr != nil {
			return fmt.Errorf("failed to list registries on network: %w", listErr)
		}

		// Filter to only include registries belonging to this cluster
		registries := filterRegistriesByClusterName(allRegistries, clusterName)

		if len(registries) == 0 {
			return nil
		}

		found = true

		// Extract names for notification
		for _, reg := range registries {
			registryNames = append(registryNames, reg.Name)
		}

		// Delete filtered registries by their info
		_, deleteErr := registryMgr.DeleteRegistriesByInfo(
			cmd.Context(),
			registries,
			deleteVolumes,
		)
		if deleteErr != nil {
			return fmt.Errorf("failed to delete registries: %w", deleteErr)
		}

		return nil
	})

	return registryNames, found, err
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

// cleanupRegistriesByNetwork discovers and cleans up all registry containers
// (both local and mirror registries) by inspecting the Docker network.
// This unified approach works for both scaffolded and non-scaffolded clusters.
// Only registries belonging to the specified cluster (by name prefix) are deleted.
func cleanupRegistriesByNetwork(
	cmd *cobra.Command,
	deps lifecycle.Deps,
	networkName string,
	clusterName string,
	deleteVolumes bool,
	cleanupDeps CleanupDependencies,
) error {
	registryNames, _, err := deleteRegistriesOnNetworkCore(
		cmd,
		networkName,
		clusterName,
		deleteVolumes,
		cleanupDeps,
	)
	if err != nil {
		return err
	}

	displayRegistryCleanupOutputWithTimer(cmd, deps.Timer, registryNames)

	return nil
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
		clusterCfg.Spec.Cluster.Provider,
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
			clusterName,
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
			clusterName,
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
			clusterName,
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

func cleanupTalosMirrorRegistries(
	cmd *cobra.Command,
	cfgManager *ksailconfigmanager.ConfigManager,
	deps lifecycle.Deps,
	clusterName string,
	deleteVolumes bool,
	cleanupDeps CleanupDependencies,
	provider v1alpha1.Provider,
) error {
	// Collect mirror specs from Talos config (not kind/mirrors directory)
	mirrorSpecs, registryNames := CollectTalosMirrorSpecs(cmd, cfgManager, provider)

	// Talos uses the cluster name as the network name
	networkName := clusterName

	// If no registry specs found from config (non-scaffolded cluster),
	// fall back to network-based discovery
	if len(registryNames) == 0 {
		return cleanupRegistriesByNetwork(
			cmd,
			deps,
			networkName,
			clusterName,
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
