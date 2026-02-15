package vclusterprovisioner

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/registry"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

// ConfigureContainerdRegistryMirrors injects hosts.toml files directly into VCluster
// nodes to configure containerd to use the local registry mirrors. This is called after
// the cluster is created and registries are connected to the network.
//
// VCluster nodes are Docker containers with containerd, so the same hosts.toml injection
// approach used by Kind works here. The function targets containers matching the
// vcluster.cp.<name> and vcluster.node.<name>.* naming convention.
func ConfigureContainerdRegistryMirrors(
	ctx context.Context,
	clusterName string,
	mirrorSpecs []registry.MirrorSpec,
	dockerClient client.APIClient,
	_ io.Writer,
) error {
	if len(mirrorSpecs) == 0 {
		return nil
	}

	entries := registry.BuildMirrorEntries(mirrorSpecs, clusterName, nil, nil, nil)
	if len(entries) == 0 {
		return nil
	}

	nodes, err := listVClusterNodes(ctx, dockerClient, clusterName)
	if err != nil {
		return err
	}

	if len(nodes) == 0 {
		return fmt.Errorf("%w: %s", ErrNoVClusterNodes, clusterName)
	}

	err = registry.InjectHostsTomlIntoNodes(
		ctx, dockerClient, nodes, entries,
	)
	if err != nil {
		return fmt.Errorf("failed to inject hosts.toml into vcluster nodes: %w", err)
	}

	return nil
}

// listVClusterNodes returns the container names of VCluster nodes for the given cluster.
// It matches containers by the vcluster.cp.<name> and vcluster.node.<name>.* naming convention.
func listVClusterNodes(
	ctx context.Context,
	dockerClient client.APIClient,
	clusterName string,
) ([]string, error) {
	containers, err := dockerClient.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	cpPrefix := controlPlaneContainerPrefix + clusterName
	nodePrefix := "vcluster.node." + clusterName + "."

	var nodes []string

	for _, c := range containers {
		for _, rawName := range c.Names {
			name := strings.TrimPrefix(rawName, "/")
			if name == cpPrefix || strings.HasPrefix(name, nodePrefix) {
				nodes = append(nodes, name)

				break
			}
		}
	}

	return nodes, nil
}

// SetupRegistries creates mirror registries based on mirror specifications.
// Registries are created without network attachment first, as the VCluster network
// doesn't exist until after the cluster is created.
func SetupRegistries(
	ctx context.Context,
	clusterName string,
	dockerClient client.APIClient,
	mirrorSpecs []registry.MirrorSpec,
	writer io.Writer,
) error {
	registryMgr, registriesInfo, err := registry.PrepareRegistryManagerFromSpecs(
		ctx, mirrorSpecs, clusterName, dockerClient,
	)
	if err != nil {
		return fmt.Errorf("failed to prepare vcluster registry manager: %w", err)
	}

	if registryMgr == nil {
		return nil
	}

	err = registry.SetupRegistries(
		ctx, registryMgr, registriesInfo, clusterName, "", writer,
	)
	if err != nil {
		return fmt.Errorf("setup vcluster registries: %w", err)
	}

	return nil
}

// ConnectRegistriesToNetwork connects existing registries to the VCluster Docker network.
// This should be called after the VCluster cluster is created and the network exists.
func ConnectRegistriesToNetwork(
	ctx context.Context,
	mirrorSpecs []registry.MirrorSpec,
	clusterName string,
	dockerClient client.APIClient,
	writer io.Writer,
) error {
	networkName := vclusterNetworkPrefix + clusterName

	err := registry.ConnectMirrorSpecsToNetwork(
		ctx, mirrorSpecs, clusterName, networkName, dockerClient, writer,
	)
	if err != nil {
		return fmt.Errorf("connect registries to vcluster network: %w", err)
	}

	return nil
}

// CleanupRegistries removes registries that are no longer in use.
func CleanupRegistries(
	ctx context.Context,
	mirrorSpecs []registry.MirrorSpec,
	clusterName string,
	dockerClient client.APIClient,
	deleteVolumes bool,
) error {
	networkName := vclusterNetworkPrefix + clusterName

	err := registry.CleanupMirrorSpecRegistries(
		ctx, mirrorSpecs, clusterName, dockerClient, deleteVolumes, networkName,
	)
	if err != nil {
		return fmt.Errorf("cleanup vcluster registries: %w", err)
	}

	return nil
}

// vclusterNetworkPrefix is the Docker network name prefix used by VCluster.
const vclusterNetworkPrefix = "vcluster."
