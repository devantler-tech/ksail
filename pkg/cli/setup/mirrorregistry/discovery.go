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

// localRegistrySuffix is the fixed tail of a cluster's local-registry container name, so a
// container called "<name>-local-registry" is evidence that a cluster called "<name>" exists.
const localRegistrySuffix = "-local-registry"

// filterRegistriesByClusterName filters registries to only include those belonging to a specific cluster.
// Registry names follow the pattern "{clusterName}-{host}" (e.g., "my-cluster-ghcr.io").
//
// The name alone is ambiguous: "foo-bar-ghcr.io" is either cluster "foo" with host "bar-ghcr.io"
// or cluster "foo-bar" with host "ghcr.io". A plain prefix test claims it for BOTH, which matters
// on a network several clusters share — every Kind cluster sits on "kind" — because the caller
// then DELETES what this returns. Deleting "foo" would take a live "foo-bar" cluster's mirrors.
//
// So a candidate is dropped when some longer cluster name, evidenced by its own local-registry
// container in this same set, also prefixes it. That is a narrowing step: it can only ever remove
// entries a bare prefix test would have returned, never add one.
//
// It is not exact ownership. A cluster with mirror registries but no local registry announces
// nothing, so its mirrors can still be misattributed — see #6294, which adds an owning-cluster
// label and makes this decidable.
func filterRegistriesByClusterName(
	registries []dockerclient.RegistryInfo,
	clusterName string,
) []dockerclient.RegistryInfo {
	if clusterName == "" {
		return registries
	}

	otherClusters := make([]string, 0, len(registries))

	for _, reg := range registries {
		name, ok := strings.CutSuffix(reg.Name, localRegistrySuffix)
		if ok && len(name) > len(clusterName) {
			otherClusters = append(otherClusters, name)
		}
	}

	prefix := clusterName + "-"
	filtered := make([]dockerclient.RegistryInfo, 0, len(registries))

	for _, reg := range registries {
		if !strings.HasPrefix(reg.Name, prefix) {
			continue
		}

		if claimedByLongerCluster(reg.Name, otherClusters) {
			continue
		}

		filtered = append(filtered, reg)
	}

	return filtered
}

// claimedByLongerCluster reports whether a longer, demonstrably-existing cluster owns this registry.
func claimedByLongerCluster(registryName string, otherClusters []string) bool {
	for _, other := range otherClusters {
		if strings.HasPrefix(registryName, other+"-") {
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
