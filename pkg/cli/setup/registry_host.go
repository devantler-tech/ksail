package setup

import (
	"context"
	"fmt"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	dockerclient "github.com/devantler-tech/ksail/v5/pkg/client/docker"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/registry"
)

// vclusterNetworkPrefix is the Docker network name prefix used by VCluster.
const vclusterNetworkPrefix = "vcluster."

// resolveRegistryHost determines the registry host to use in ArgoCD repository URLs.
//
// For VCluster distributions, pods use CoreDNS which cannot resolve Docker container
// names. This function inspects the registry container to get its Docker IP address
// on the VCluster network. For all other distributions, it returns an empty string
// to indicate the default container name should be used.
func resolveRegistryHost(
	ctx context.Context,
	clusterCfg *v1alpha1.Cluster,
	clusterName string,
) (string, error) {
	if !needsRegistryIPResolution(clusterCfg) {
		return "", nil
	}

	containerName := registry.BuildLocalRegistryName(clusterName)
	networkName := vclusterNetworkPrefix + clusterName

	dockerClient, err := dockerclient.GetDockerClient()
	if err != nil {
		return "", fmt.Errorf("create docker client: %w", err)
	}

	defer func() { _ = dockerClient.Close() }()

	registryIP, err := dockerclient.ResolveContainerIPOnNetwork(
		ctx, dockerClient, containerName, networkName,
	)
	if err != nil {
		return "", fmt.Errorf("resolve registry IP on network %s: %w", networkName, err)
	}

	return registryIP, nil
}

// needsRegistryIPResolution returns true when the distribution requires resolving
// the registry container's Docker IP instead of using its container name.
func needsRegistryIPResolution(clusterCfg *v1alpha1.Cluster) bool {
	if clusterCfg == nil {
		return false
	}

	if clusterCfg.Spec.Cluster.Distribution != v1alpha1.DistributionVCluster {
		return false
	}

	// External registries have their own DNS — no Docker IP needed.
	if clusterCfg.Spec.Cluster.LocalRegistry.IsExternal() {
		return false
	}

	return clusterCfg.Spec.Cluster.LocalRegistry.Enabled()
}
