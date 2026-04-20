package mirrorregistry

import (
	"errors"
	"fmt"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	dockerhelpers "github.com/devantler-tech/ksail/v7/pkg/cli/dockerutil"
	"github.com/devantler-tech/ksail/v7/pkg/cli/flags"
	"github.com/devantler-tech/ksail/v7/pkg/cli/setup/localregistry"
	dockerclient "github.com/devantler-tech/ksail/v7/pkg/client/docker"
	"github.com/devantler-tech/ksail/v7/pkg/notify"
	"github.com/devantler-tech/ksail/v7/pkg/timer"
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
		DockerInvoker:     dockerhelpers.WithDockerClient,
		LocalRegistryDeps: localregistry.DefaultDependencies(),
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
	networkName := GetNetworkNameForDistribution(distribution, clusterName)

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

// GetNetworkNameForDistribution returns the Docker network name for a given distribution.
func GetNetworkNameForDistribution(distribution v1alpha1.Distribution, clusterName string) string {
	switch distribution {
	case v1alpha1.DistributionVanilla:
		return "kind"
	case v1alpha1.DistributionK3s:
		return "k3d-" + clusterName
	case v1alpha1.DistributionTalos:
		return clusterName
	case v1alpha1.DistributionVCluster:
		return "vcluster." + clusterName
	case v1alpha1.DistributionKWOK:
		return "kwok-" + clusterName
	case v1alpha1.DistributionEKS:
		// EKS does not use a local Docker network.
		return ""
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

	outputTimer := flags.MaybeTimer(cmd, tmr)

	notify.WriteMessage(notify.Message{
		Type:    notify.SuccessType,
		Content: "registries deleted",
		Timer:   outputTimer,
		Writer:  cmd.OutOrStdout(),
	})
}
