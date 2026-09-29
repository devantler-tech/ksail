package vclusterprovisioner

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

	dockerclient "github.com/devantler-tech/ksail/v7/pkg/client/docker"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/registry"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/pkg/stdcopy"
)

// VCluster's containerd 2.x image service does not read certs.d when its CRI
// registry config_path is empty. Keep the node's effective configuration, set
// that path, and restart containerd before workloads are installed.
const enableContainerdRegistryHosts = "set -eu\nconfig_dir=${1:?containerd configuration directory is required}\nsnapshot=$(mktemp \"$config_dir/ksail-config-XXXXXX\")\ncandidate=$(mktemp \"$config_dir/ksail-mirrors-XXXXXX\")\ntrap 'rm -f \"$snapshot\" \"$candidate\"' EXIT\ncontainerd config dump > \"$snapshot\"\nhas_registry_hosts_path() {\n  awk '\n    /^[[:space:]]*\\[plugins[.]/ {\n      registry = ($0 ~ /cri[.]v1[.]images.*[.]registry\\]$/ ||\n                  $0 ~ /grpc[.]v1[.]cri.*[.]registry\\]$/)\n      next\n    }\n    /^\\[/ { registry = 0 }\n    registry && /config_path[[:space:]]*=/ {\n      path = $0\n      sub(/^[^=]*=/, \"\", path)\n      gsub(/[\"\\047[:space:]]/, \"\", path)\n      n = split(path, parts, \":\")\n      for (i = 1; i <= n; i++) found = found || parts[i] == \"/etc/containerd/certs.d\"\n    }\n    END { exit !found }\n  ' \"$1\"\n}\nawk '\n  /^[[:space:]]*\\[/ {\n    registry = ($0 ~ /io[.]containerd[.]cri[.]v1[.]images.*[.]registry\\]$/ ||\n                $0 ~ /io[.]containerd[.]grpc[.]v1[.]cri.*[.]registry\\]$/)\n  }\n  registry && /^[[:space:]]*config_path[[:space:]]*=/ {\n    found = 1\n    value = $0\n    sub(/^[^=]*=[[:space:]]*/, \"\", value)\n    sub(/[[:space:]]*$/, \"\", value)\n    if (value ~ /^[\"\\047].*[\"\\047]$/) value = substr(value, 2, length(value) - 2)\n    count = split(value, paths, \":\")\n    has_hosts = 0\n    for (i = 1; i <= count; i++) {\n      if (paths[i] == \"/etc/containerd/certs.d\") has_hosts = 1\n    }\n    if (!has_hosts) {\n      if (value != \"\") value = value \":\"\n      sub(/=.*/, \"= \\\"\" value \"/etc/containerd/certs.d\\\"\")\n    }\n  { print }\n  END { if (!found) exit 1 }\n' \"$snapshot\" > \"$candidate\"\nif cmp -s \"$snapshot\" \"$candidate\"; then\n  exit 0\nfi\nchmod 0600 \"$candidate\"\nmv \"$candidate\" \"$config_dir/config.toml\"\nsystemctl restart containerd\nsystemctl is-active --quiet containerd\nconfig dump > \"$snapshot\"\nif ! has_registry_hosts_path \"$snapshot\"; then\n  echo 'containerd effective registry configuration lacks the mirror hosts path' >&2\n  exit 1\nfi"

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
	dockerClient dockerclient.Client,
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

	for _, node := range nodes {
		err := enableRegistryHostsOnNode(ctx, dockerClient, node)
		if err != nil {
			return fmt.Errorf("failed to enable registry hosts on vcluster node %s: %w", node, err)
		}
	}

	return nil
}

func enableRegistryHostsOnNode(
	ctx context.Context,
	dockerClient dockerclient.Client,
	node string,
) error {
	execID, err := dockerClient.ContainerExecCreate(ctx, node, container.ExecOptions{
		Cmd: []string{
			"sh",
			"-c",
			enableContainerdRegistryHosts,
			"ksail",
			"/etc/containerd",
		},
		AttachStdout: true,
		AttachStderr: true,
	})
	if err != nil {
		return fmt.Errorf("create containerd configuration exec: %w", err)
	}

	response, err := dockerClient.ContainerExecAttach(ctx, execID.ID, container.ExecStartOptions{})
	if err != nil {
		return fmt.Errorf("attach containerd configuration exec: %w", err)
	}
	defer response.Close()

	var stderr bytes.Buffer

	_, copyErr := stdcopy.StdCopy(io.Discard, &stderr, response.Reader)
	if copyErr != nil {
		return fmt.Errorf("read containerd configuration exec: %w", copyErr)
	}

	result, err := dockerClient.ContainerExecInspect(ctx, execID.ID)
	if err != nil {
		return fmt.Errorf("inspect containerd configuration exec: %w", err)
	}

	if result.Running {
		return fmt.Errorf("%w: containerd configuration exec still running", registry.ErrExecFailed)
	}

	if result.ExitCode != 0 {
		return fmt.Errorf(
			"%w with exit code %d: %s",
			registry.ErrExecFailed,
			result.ExitCode,
			stderr.String(),
		)
	}

	return nil
}

// listVClusterNodes returns the container names of VCluster nodes for the given cluster.
// It matches containers by the vcluster.cp.<name> and vcluster.node.<name>.* naming convention.
func listVClusterNodes(
	ctx context.Context,
	dockerClient dockerclient.Client,
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
	dockerClient dockerclient.Client,
	mirrorSpecs []registry.MirrorSpec,
	writer io.Writer,
) error {
	err := registry.SetupMirrorSpecRegistries(
		ctx, mirrorSpecs, clusterName, dockerClient, "", writer,
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
	dockerClient dockerclient.Client,
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
	dockerClient dockerclient.Client,
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
