package mirrorregistry

import (
	"fmt"
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	dockerclient "github.com/devantler-tech/ksail/v7/pkg/client/docker"
	"github.com/spf13/cobra"
)

// DiscoveredRegistries holds registry information discovered before cluster deletion.
// This is used when the network will be destroyed during cluster deletion (e.g., Talos).
type DiscoveredRegistries struct {
	Registries []dockerclient.RegistryInfo
}

// filterRegistriesByClusterName filters registries to only include those belonging to a specific cluster.
// Registry names follow the pattern "{clusterName}-{host}" (e.g., "my-cluster-ghcr.io").
func filterRegistriesByClusterName(
	registries []dockerclient.RegistryInfo,
	clusterName string,
) []dockerclient.RegistryInfo {
	if clusterName == "" {
		return registries
	}

	prefix := clusterName + "-"
	filtered := make([]dockerclient.RegistryInfo, 0, len(registries))

	for _, reg := range registries {
		if strings.HasPrefix(reg.Name, prefix) {
			filtered = append(filtered, reg)
		}
	}

	return filtered
}

// DiscoverClusterRegistryRemnant finds the KSail-owned registry containers belonging to a
// cluster WITHOUT consulting any Docker network.
//
// Network-based discovery is blind to a partial teardown: once the cluster network is gone
// (or the distribution was misdetected, so the wrong network name is resolved) the registries
// are invisible and leak. Matching on KSail ownership plus the "<cluster>-" name prefix stays
// correct in that state.
//
// Only KSail-owned containers are ever returned, so a caller acting on this can never remove a
// registry KSail did not create.
func DiscoverClusterRegistryRemnant(
	cmd *cobra.Command,
	clusterName string,
	cleanupDeps CleanupDependencies,
) *DiscoveredRegistries {
	if clusterName == "" {
		return &DiscoveredRegistries{}
	}

	var registries []dockerclient.RegistryInfo

	err := cleanupDeps.DockerInvoker(cmd, func(dockerClient dockerclient.Client) error {
		registryMgr, mgrErr := dockerclient.NewRegistryManager(dockerClient)
		if mgrErr != nil {
			return fmt.Errorf("create registry manager: %w", mgrErr)
		}

		discovered, listErr := registryMgr.ListAllRegistries(cmd.Context())
		if listErr != nil {
			return fmt.Errorf("list registries: %w", listErr)
		}

		owned := make([]dockerclient.RegistryInfo, 0, len(discovered))

		for _, reg := range discovered {
			if reg.IsKSailOwned {
				owned = append(owned, reg)
			}
		}

		registries = filterRegistriesByClusterName(owned, clusterName)

		return nil
	})
	if err != nil {
		cmd.PrintErrf(
			"Warning: failed to discover registries for cluster %q: %v\n",
			clusterName,
			err,
		)
	}

	return &DiscoveredRegistries{Registries: registries}
}

// DiscoverRegistriesByNetwork finds all registries connected to the cluster network.
// This is a simplified version that doesn't require a cluster config object.
// Registries are filtered to only include those belonging to the specified cluster.
func DiscoverRegistriesByNetwork(
	cmd *cobra.Command,
	distribution v1alpha1.Distribution,
	clusterName string,
	cleanupDeps CleanupDependencies,
) *DiscoveredRegistries {
	networkName := GetNetworkNameForDistribution(distribution, clusterName)

	var registries []dockerclient.RegistryInfo

	err := cleanupDeps.DockerInvoker(cmd, func(dockerClient dockerclient.Client) error {
		registryMgr, mgrErr := dockerclient.NewRegistryManager(dockerClient)
		if mgrErr != nil {
			return fmt.Errorf("create registry manager: %w", mgrErr)
		}

		discovered, listErr := registryMgr.ListRegistriesOnNetwork(cmd.Context(), networkName)
		if listErr != nil {
			return fmt.Errorf("list registries on network: %w", listErr)
		}

		registries = filterRegistriesByClusterName(discovered, clusterName)

		return nil
	})
	if err != nil {
		cmd.PrintErrf(
			"Warning: failed to discover registries on network %q: %v\n",
			networkName,
			err,
		)
	}

	return &DiscoveredRegistries{Registries: registries}
}
