package mirrorregistry

import (
	"fmt"
	"strings"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	dockerclient "github.com/devantler-tech/ksail/v5/pkg/client/docker"
	"github.com/docker/docker/client"
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
// Registries are filtered to only include those belonging to the specified cluster.
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

		// Filter to only include registries belonging to this cluster
		registries = filterRegistriesByClusterName(discovered, clusterName)

		return nil
	})

	return &DiscoveredRegistries{Registries: registries}
}
