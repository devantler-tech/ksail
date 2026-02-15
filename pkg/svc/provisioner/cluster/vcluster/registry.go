package vclusterprovisioner

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"strings"

	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/registry"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
)

// randomDelimiterBytes is the number of random bytes used to generate heredoc delimiters.
// 8 bytes produces 16 hex characters, making collisions with user content extremely unlikely.
const randomDelimiterBytes = 8

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

	return injectHostsTomlIntoNodes(ctx, dockerClient, nodes, entries)
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

// injectHostsTomlIntoNodes injects hosts.toml files into all VCluster nodes for the given entries.
func injectHostsTomlIntoNodes(
	ctx context.Context,
	dockerClient client.APIClient,
	nodes []string,
	entries []registry.MirrorEntry,
) error {
	for _, entry := range entries {
		hostsTomlContent := registry.GenerateHostsToml(entry)

		for _, node := range nodes {
			err := injectHostsToml(ctx, dockerClient, node, entry.Host, hostsTomlContent)
			if err != nil {
				return fmt.Errorf(
					"failed to inject hosts.toml for %s into node %s: %w",
					entry.Host,
					node,
					err,
				)
			}
		}
	}

	return nil
}

// injectHostsToml creates the hosts directory and writes the hosts.toml file inside a VCluster node.
func injectHostsToml(
	ctx context.Context,
	dockerClient client.APIClient,
	nodeName string,
	registryHost string,
	hostsTomlContent string,
) error {
	certsDir := "/etc/containerd/certs.d/" + registryHost
	escapedCertsDir := escapeShellArg(certsDir)

	delimiter, err := generateRandomDelimiter()
	if err != nil {
		return err
	}

	cmd := []string{
		"sh", "-c",
		fmt.Sprintf("mkdir -p %s && cat > %s/hosts.toml << '%s'\n%s\n%s",
			escapedCertsDir, escapedCertsDir, delimiter, hostsTomlContent, delimiter),
	}

	execConfig := container.ExecOptions{
		Cmd:          cmd,
		AttachStdout: true,
		AttachStderr: true,
	}

	execID, err := dockerClient.ContainerExecCreate(ctx, nodeName, execConfig)
	if err != nil {
		return fmt.Errorf("failed to create exec: %w", err)
	}

	resp, err := dockerClient.ContainerExecAttach(ctx, execID.ID, container.ExecStartOptions{})
	if err != nil {
		return fmt.Errorf("failed to attach to exec: %w", err)
	}
	defer resp.Close()

	var stdout, stderr bytes.Buffer

	_, _ = stdcopy.StdCopy(&stdout, &stderr, resp.Reader)

	inspectResp, err := dockerClient.ContainerExecInspect(ctx, execID.ID)
	if err != nil {
		return fmt.Errorf("failed to inspect exec: %w", err)
	}

	if inspectResp.ExitCode != 0 {
		return fmt.Errorf(
			"%w with exit code %d: %s",
			ErrExecFailed,
			inspectResp.ExitCode,
			stderr.String(),
		)
	}

	return nil
}

// generateRandomDelimiter creates a random heredoc delimiter to prevent injection attacks.
func generateRandomDelimiter() (string, error) {
	randomBytes := make([]byte, randomDelimiterBytes)

	_, err := rand.Read(randomBytes)
	if err != nil {
		return "", fmt.Errorf("failed to generate random delimiter: %w", err)
	}

	return "EOF_" + hex.EncodeToString(randomBytes), nil
}

// escapeShellArg escapes a string for safe use in POSIX shell commands.
func escapeShellArg(arg string) string {
	escaped := strings.ReplaceAll(arg, "'", "'\\''")

	return "'" + escaped + "'"
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
	registryMgr, registriesInfo, err := prepareRegistryManager(
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
	if len(mirrorSpecs) == 0 {
		return nil
	}

	registriesInfo := registry.BuildRegistryInfosFromSpecs(mirrorSpecs, nil, nil, clusterName)
	if len(registriesInfo) == 0 {
		return nil
	}

	networkName := vclusterNetworkPrefix + clusterName

	err := registry.ConnectRegistriesToNetwork(
		ctx, dockerClient, registriesInfo, networkName, writer,
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
	registryMgr, registriesInfo, err := prepareRegistryManager(
		ctx, mirrorSpecs, clusterName, dockerClient,
	)
	if err != nil {
		return fmt.Errorf("failed to prepare registry manager for cleanup: %w", err)
	}

	if registryMgr == nil {
		return nil
	}

	networkName := vclusterNetworkPrefix + clusterName

	err = registry.CleanupRegistries(
		ctx, registryMgr, registriesInfo, clusterName, deleteVolumes, networkName, nil,
	)
	if err != nil {
		return fmt.Errorf("cleanup vcluster registries: %w", err)
	}

	return nil
}

// vclusterNetworkPrefix is the Docker network name prefix used by VCluster.
const vclusterNetworkPrefix = "vcluster."

// prepareRegistryManager creates a registry manager and builds registry infos
// for VCluster registry operations. Returns nil manager if mirrorSpecs is empty.
func prepareRegistryManager(
	ctx context.Context,
	mirrorSpecs []registry.MirrorSpec,
	clusterName string,
	dockerClient client.APIClient,
) (registry.Backend, []registry.Info, error) {
	if len(mirrorSpecs) == 0 {
		return nil, nil, nil
	}

	upstreams := registry.BuildUpstreamLookup(mirrorSpecs)

	registryMgr, infos, err := registry.PrepareRegistryManager(
		ctx,
		dockerClient,
		func(usedPorts map[int]struct{}) []registry.Info {
			return registry.BuildRegistryInfosFromSpecs(
				mirrorSpecs,
				upstreams,
				usedPorts,
				clusterName,
			)
		},
	)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to prepare registry manager: %w", err)
	}

	return registryMgr, infos, nil
}
