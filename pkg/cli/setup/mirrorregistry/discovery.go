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

// localRegistrySuffix is the fixed tail of a cluster's local-registry container name. A container
// called "<name>-local-registry" is therefore proof that a cluster called "<name>" exists.
const localRegistrySuffix = "-local-registry"

// attributeRegistriesToCluster returns the registries that belong to clusterName, resolving the
// ambiguity inherent in KSail's registry naming.
//
// Registry names are "<cluster>-<host>", which a plain prefix test cannot decode: given
// "foo-bar-docker.io", the cluster could be "foo" (host "bar-docker.io") or "foo-bar" (host
// "docker.io"). A prefix test claims it for BOTH, so tearing down "foo" would remove a live
// "foo-bar" cluster's registries.
//
// Every cluster with a local registry contributes a "<cluster>-local-registry" container, so the
// set of those names is the set of known clusters. Attributing each registry to the LONGEST known
// cluster name that prefixes it resolves the ambiguity: "foo-bar-docker.io" goes to "foo-bar"
// whenever "foo-bar" is known, and only falls to "foo" when no such cluster exists.
func attributeRegistriesToCluster(
	registries []dockerclient.RegistryInfo,
	clusterName string,
) []dockerclient.RegistryInfo {
	if clusterName == "" {
		return registries
	}

	// Known clusters, from the local-registry containers present on the host.
	known := make([]string, 0, len(registries))

	for _, reg := range registries {
		if before, ok := strings.CutSuffix(reg.Name, localRegistrySuffix); ok {
			known = append(known, before)
		}
	}

	prefix := clusterName + "-"
	filtered := make([]dockerclient.RegistryInfo, 0, len(registries))

	for _, reg := range registries {
		if reg.Name == clusterName+localRegistrySuffix {
			filtered = append(filtered, reg)

			continue
		}

		if !strings.HasPrefix(reg.Name, prefix) {
			continue
		}

		// Claimed by a more specific cluster that actually exists — leave it alone.
		if ownedByLongerCluster(reg.Name, clusterName, known) {
			continue
		}

		filtered = append(filtered, reg)
	}

	return filtered
}

// ownedByLongerCluster reports whether some known cluster with a longer name than clusterName also
// prefixes registryName, which makes that cluster the rightful owner.
func ownedByLongerCluster(registryName, clusterName string, known []string) bool {
	for _, other := range known {
		if len(other) > len(clusterName) && strings.HasPrefix(registryName, other+"-") {
			return true
		}
	}

	return false
}

// DiscoverClusterRegistryRemnant finds the KSail-owned registry containers belonging to a
// cluster WITHOUT consulting any Docker network.
//
// Network-based discovery is blind to a partial teardown: once the cluster network is gone
// (or the distribution was misdetected, so the wrong network name is resolved) the registries
// are invisible and leak. Matching on KSail ownership plus cluster attribution stays correct in
// that state.
//
// Only KSail-owned containers attributed to THIS cluster are ever returned, so a caller acting on
// this can neither remove a registry KSail did not create nor one belonging to another cluster.
func DiscoverClusterRegistryRemnant(
	cmd *cobra.Command,
	clusterName string,
	cleanupDeps CleanupDependencies,
) *DiscoveredRegistries {
	if clusterName == "" {
		return &DiscoveredRegistries{}
	}

	return discoverRegistries(
		cmd,
		clusterName,
		cleanupDeps,
		"for cluster "+clusterName,
		func(registryMgr *dockerclient.RegistryManager) ([]dockerclient.RegistryInfo, error) {
			discovered, err := registryMgr.ListAllRegistries(cmd.Context())
			if err != nil {
				return nil, fmt.Errorf("list registries: %w", err)
			}

			owned := make([]dockerclient.RegistryInfo, 0, len(discovered))

			for _, reg := range discovered {
				if reg.IsKSailOwned {
					owned = append(owned, reg)
				}
			}

			return owned, nil
		},
		attributeRegistriesToCluster,
	)
}

// DiscoverRegistriesToDisconnect finds the registries that must be detached from the cluster's
// network before the cluster is destroyed.
//
// Membership of the cluster's network — not the container name — is what decides this. A local
// mirror endpoint configured without a prefix keeps its bare name (K3d permits e.g. "docker.io"),
// so a name-scoped filter silently skips it and K3d is then unable to remove its still-populated
// network.
//
// The one exclusion is a registry attributable to a DIFFERENT cluster, which matters only on a
// network several clusters share: every Kind cluster sits on the single "kind" network, and
// detaching another cluster's registries from it would break a live cluster.
func DiscoverRegistriesToDisconnect(
	cmd *cobra.Command,
	distribution v1alpha1.Distribution,
	clusterName string,
	cleanupDeps CleanupDependencies,
) *DiscoveredRegistries {
	networkName := GetNetworkNameForDistribution(distribution, clusterName)
	if networkName == "" {
		return &DiscoveredRegistries{}
	}

	var registries []dockerclient.RegistryInfo

	err := cleanupDeps.DockerInvoker(cmd, func(dockerClient dockerclient.Client) error {
		registryMgr, mgrErr := dockerclient.NewRegistryManager(dockerClient)
		if mgrErr != nil {
			return fmt.Errorf("create registry manager: %w", mgrErr)
		}

		// The full host list is what makes attribution possible; the network list is what is
		// actually attached.
		all, listErr := registryMgr.ListAllRegistries(cmd.Context())
		if listErr != nil {
			return fmt.Errorf("list registries: %w", listErr)
		}

		onNetwork, netErr := registryMgr.ListRegistriesOnNetwork(cmd.Context(), networkName)
		if netErr != nil {
			return fmt.Errorf("list registries on network: %w", netErr)
		}

		otherClusters := knownClustersExcept(all, clusterName)

		for _, reg := range onNetwork {
			if !reg.IsKSailOwned || belongsToAnyCluster(reg.Name, otherClusters) {
				continue
			}

			registries = append(registries, reg)
		}

		return nil
	})
	if err != nil {
		cmd.PrintErrf(
			"Warning: failed to discover registries to disconnect from %q: %v\n",
			networkName,
			err,
		)
	}

	return &DiscoveredRegistries{Registries: registries}
}

// knownClustersExcept lists the clusters evidenced by a local-registry container, minus the one
// being torn down.
func knownClustersExcept(
	registries []dockerclient.RegistryInfo,
	clusterName string,
) []string {
	others := make([]string, 0, len(registries))

	for _, reg := range registries {
		if !strings.HasSuffix(reg.Name, localRegistrySuffix) {
			continue
		}

		name := strings.TrimSuffix(reg.Name, localRegistrySuffix)
		if name != clusterName {
			others = append(others, name)
		}
	}

	return others
}

// belongsToAnyCluster reports whether registryName is one of the given clusters' registries.
func belongsToAnyCluster(registryName string, clusters []string) bool {
	for _, c := range clusters {
		if registryName == c+localRegistrySuffix || strings.HasPrefix(registryName, c+"-") {
			return true
		}
	}

	return false
}

// registryLister produces the candidate registries a discovery variant considers, before they are
// narrowed to a single cluster.
type registryLister func(*dockerclient.RegistryManager) ([]dockerclient.RegistryInfo, error)

// clusterNarrower selects the registries belonging to a cluster from a wider set.
type clusterNarrower func(
	regs []dockerclient.RegistryInfo,
	clusterName string,
) []dockerclient.RegistryInfo

// discoverRegistries runs a registry listing through the Docker client and narrows the result to
// the registries belonging to clusterName.
//
// narrow decides HOW ownership is judged, and the choice is load-bearing:
// filterRegistriesByClusterName is a bare prefix test, adequate once membership of a
// cluster-specific network has already constrained the set; attributeRegistriesToCluster is
// collision-safe and is what every REMOVAL path must use.
//
// subject describes what was being looked for, and appears only in the failure warning.
func discoverRegistries(
	cmd *cobra.Command,
	clusterName string,
	cleanupDeps CleanupDependencies,
	subject string,
	list registryLister,
	narrow clusterNarrower,
) *DiscoveredRegistries {
	var registries []dockerclient.RegistryInfo

	err := cleanupDeps.DockerInvoker(cmd, func(dockerClient dockerclient.Client) error {
		registryMgr, mgrErr := dockerclient.NewRegistryManager(dockerClient)
		if mgrErr != nil {
			return fmt.Errorf("create registry manager: %w", mgrErr)
		}

		discovered, listErr := list(registryMgr)
		if listErr != nil {
			return listErr
		}

		registries = narrow(discovered, clusterName)

		return nil
	})
	if err != nil {
		cmd.PrintErrf("Warning: failed to discover registries %s: %v\n", subject, err)
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

	return discoverRegistries(
		cmd,
		clusterName,
		cleanupDeps,
		"on network "+networkName,
		func(registryMgr *dockerclient.RegistryManager) ([]dockerclient.RegistryInfo, error) {
			discovered, err := registryMgr.ListRegistriesOnNetwork(cmd.Context(), networkName)
			if err != nil {
				return nil, fmt.Errorf("list registries on network: %w", err)
			}

			return discovered, nil
		},
		filterRegistriesByClusterName,
	)
}
