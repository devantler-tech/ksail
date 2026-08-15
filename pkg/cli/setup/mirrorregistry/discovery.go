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
//
// The name alone is ambiguous: "foo-bar-ghcr.io" is either cluster "foo" with host "bar-ghcr.io"
// or cluster "foo-bar" with host "ghcr.io". A plain prefix test claims it for BOTH, which matters
// on a network several clusters share — every Kind cluster sits on "kind" — because the caller
// then DELETES what this returns. Deleting "foo" would take a live "foo-bar" cluster's mirrors.
//
// otherClusters resolves that: it is the set of clusters that DEMONSTRABLY EXIST, discovered from
// their node containers rather than inferred from registry names, and a candidate claimed by a
// longer name in that set is left alone. Passing an empty set restores the plain prefix behaviour.
//
// Deriving rivals from names instead would misfire on a legitimate configuration: a cluster "foo"
// with a mirror host called "bar-local-registry" produces a container "foo-bar-local-registry",
// which reads as a cluster "foo-bar" that does not exist — and its own mirrors would then be
// skipped and leaked. Mirror hosts are user-configurable, so no name pattern is a safe existence
// marker.
//
// This narrows: it only ever removes entries the plain prefix test would have returned, never adds
// one. It is still not exact ownership — #6294 adds an owning-cluster label for that.
func filterRegistriesByClusterName(
	registries []dockerclient.RegistryInfo,
	clusterName string,
	otherClusters []string,
) []dockerclient.RegistryInfo {
	if clusterName == "" {
		return registries
	}

	prefix := clusterName + "-"
	filtered := make([]dockerclient.RegistryInfo, 0, len(registries))

	for _, reg := range registries {
		if !strings.HasPrefix(reg.Name, prefix) {
			continue
		}

		if claimedByLongerCluster(reg.Name, clusterName, otherClusters) {
			continue
		}

		filtered = append(filtered, reg)
	}

	return filtered
}

// claimedByLongerCluster reports whether an existing cluster with a longer name owns this registry.
func claimedByLongerCluster(registryName, clusterName string, otherClusters []string) bool {
	for _, other := range otherClusters {
		if len(other) > len(clusterName) && strings.HasPrefix(registryName, other+"-") {
			return true
		}
	}

	return false
}

// DiscoverRegistriesByNetwork finds all registries connected to the cluster network.
// This is a simplified version that doesn't require a cluster config object.
// Registries are filtered to only include those belonging to the specified cluster.
func DiscoverRegistriesByNetwork(
	cmd *cobra.Command,
	distribution v1alpha1.Distribution,
	clusterName string,
	otherClusters []string,
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

		registries = filterRegistriesByClusterName(discovered, clusterName, otherClusters)

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
