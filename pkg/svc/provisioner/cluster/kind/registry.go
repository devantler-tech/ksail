package kindprovisioner

import (
	"context"
	"fmt"
	"io"
	"strings"

	kindconfigmanager "github.com/devantler-tech/ksail/v5/pkg/fsutil/configmanager/kind"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/registry"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

// ConfigureContainerdRegistryMirrors injects hosts.toml files directly into Kind nodes
// to configure containerd to use the local registry mirrors. This is called after the
// cluster is created and registries are connected to the network.
//
// This approach doesn't require file mounts or declarative configuration - it works
// purely in-memory by executing commands inside the Kind nodes via Docker.
//
// If scaffolded hosts.toml files are already mounted via extraMounts in the Kind config,
// this function skips injection for those hosts to avoid read-only filesystem errors.
func ConfigureContainerdRegistryMirrors(
	ctx context.Context,
	kindConfig *v1alpha4.Cluster,
	mirrorSpecs []registry.MirrorSpec,
	dockerClient client.APIClient,
	_ io.Writer,
) error {
	entriesToInject := getEntriesToInject(kindConfig, mirrorSpecs)
	if len(entriesToInject) == 0 {
		return nil
	}

	nodes, err := getKindNodesForCluster(ctx, dockerClient, kindConfig)
	if err != nil {
		return err
	}

	err = registry.InjectHostsTomlIntoNodes(
		ctx, dockerClient, nodes, entriesToInject,
	)
	if err != nil {
		return fmt.Errorf("failed to inject hosts.toml into kind nodes: %w", err)
	}

	return nil
}

// getEntriesToInject returns mirror entries that need to be injected into Kind nodes.
// It filters out entries that already have extraMounts configured in the Kind config.
func getEntriesToInject(
	kindConfig *v1alpha4.Cluster,
	mirrorSpecs []registry.MirrorSpec,
) []registry.MirrorEntry {
	if len(mirrorSpecs) == 0 {
		return nil
	}

	// Get cluster name to use as container prefix (must match how registries were created)
	clusterName := "kind"
	if kindConfig != nil && kindConfig.Name != "" {
		clusterName = kindConfig.Name
	}

	mountedHosts := buildMountedHostsSet(kindConfig)
	entries := registry.BuildMirrorEntries(mirrorSpecs, clusterName, nil, nil, nil)

	var entriesToInject []registry.MirrorEntry

	for _, entry := range entries {
		if !mountedHosts[entry.Host] {
			entriesToInject = append(entriesToInject, entry)
		}
	}

	return entriesToInject
}

// getKindNodesForCluster returns the list of Kind nodes for the given cluster configuration.
func getKindNodesForCluster(
	ctx context.Context,
	dockerClient client.APIClient,
	kindConfig *v1alpha4.Cluster,
) ([]string, error) {
	clusterName := "kind"
	if kindConfig != nil && kindConfig.Name != "" {
		clusterName = kindConfig.Name
	}

	nodes, err := listKindNodes(ctx, dockerClient, clusterName)
	if err != nil {
		return nil, fmt.Errorf("failed to list Kind nodes: %w", err)
	}

	if len(nodes) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrNoKindNodes, clusterName)
	}

	return nodes, nil
}

// buildMountedHostsSet returns a set of registry hosts that have extraMounts configured
// in the Kind config. These hosts have scaffolded hosts.toml files that will be mounted
// directly into the containers.
func buildMountedHostsSet(kindConfig *v1alpha4.Cluster) map[string]bool {
	mountedHosts := make(map[string]bool)

	if kindConfig == nil {
		return mountedHosts
	}

	certsPrefix := "/etc/containerd/certs.d/"

	for _, node := range kindConfig.Nodes {
		for _, mount := range node.ExtraMounts {
			// Check if this mount is for containerd certs.d
			if after, ok := strings.CutPrefix(mount.ContainerPath, certsPrefix); ok {
				// Extract the registry host from the path
				// e.g., /etc/containerd/certs.d/docker.io -> docker.io
				host := after

				host = strings.TrimSuffix(host, "/")
				if host != "" {
					mountedHosts[host] = true
				}
			}
		}
	}

	return mountedHosts
}

// listKindNodes returns the container IDs/names of Kind nodes for the given cluster.
func listKindNodes(
	ctx context.Context,
	dockerClient client.APIClient,
	clusterName string,
) ([]string, error) {
	containers, err := dockerClient.ContainerList(ctx, container.ListOptions{
		All: true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	var nodes []string

	labelKey := "io.x-k8s.kind.cluster"

	for _, c := range containers {
		if c.Labels[labelKey] == clusterName {
			// Use the first name (without leading slash)
			if len(c.Names) > 0 {
				name := strings.TrimPrefix(c.Names[0], "/")
				nodes = append(nodes, name)
			}
		}
	}

	return nodes, nil
}

// EscapeShellArg escapes a string for safe use in POSIX shell commands.
// It wraps the string in single quotes and escapes any single quotes within.
func EscapeShellArg(arg string) string {
	return registry.EscapeShellArg(arg)
}

// SetupRegistries creates mirror registries based on mirror specifications.
// Registries are created without network attachment first, as the "kind" network
// doesn't exist until after the cluster is created. mirrorSpecs should contain the
// user-supplied mirror definitions so upstream URLs can be preserved when creating
// local proxy registry.
func SetupRegistries(
	ctx context.Context,
	_ *v1alpha4.Cluster,
	clusterName string,
	dockerClient client.APIClient,
	mirrorSpecs []registry.MirrorSpec,
	writer io.Writer,
) error {
	err := registry.SetupMirrorSpecRegistries(
		ctx, mirrorSpecs, clusterName, dockerClient,
		kindconfigmanager.DefaultNetworkName, writer,
	)
	if err != nil {
		return fmt.Errorf("failed to setup kind registries: %w", err)
	}

	return nil
}

// ConnectRegistriesToNetwork connects existing registries to the Kind network.
// This should be called after the Kind cluster is created and the "kind" network exists.
func ConnectRegistriesToNetwork(
	ctx context.Context,
	mirrorSpecs []registry.MirrorSpec,
	clusterName string,
	dockerClient client.APIClient,
	writer io.Writer,
) error {
	errConnect := registry.ConnectMirrorSpecsToNetwork(
		ctx, mirrorSpecs, clusterName,
		kindconfigmanager.DefaultNetworkName, dockerClient, writer,
	)
	if errConnect != nil {
		return fmt.Errorf("failed to connect kind registries to network: %w", errConnect)
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
	err := registry.CleanupMirrorSpecRegistries(
		ctx, mirrorSpecs, clusterName, dockerClient,
		deleteVolumes, kindconfigmanager.DefaultNetworkName,
	)
	if err != nil {
		return fmt.Errorf("failed to cleanup kind registries: %w", err)
	}

	return nil
}
