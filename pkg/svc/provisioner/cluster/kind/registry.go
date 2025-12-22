package kindprovisioner

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

	dockerclient "github.com/devantler-tech/ksail/v5/pkg/client/docker"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/registry"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

const kindNetworkName = "kind"

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
	writer io.Writer,
) error {
	if len(mirrorSpecs) == 0 {
		return nil
	}

	// Build a set of registry hosts that already have mounts configured
	// If a host has an extraMount, the scaffolded hosts.toml is already in place
	mountedHosts := buildMountedHostsSet(kindConfig)

	// Build mirror entries to get the hosts.toml content
	entries := registry.BuildMirrorEntries(mirrorSpecs, "", nil, nil, nil)
	if len(entries) == 0 {
		return nil
	}

	// Filter out entries that are already mounted
	var entriesToInject []registry.MirrorEntry
	for _, entry := range entries {
		if !mountedHosts[entry.Host] {
			entriesToInject = append(entriesToInject, entry)
		}
	}

	// If all entries are already mounted, nothing to do
	if len(entriesToInject) == 0 {
		return nil
	}

	// Get the cluster name to find nodes
	clusterName := "kind"
	if kindConfig != nil && kindConfig.Name != "" {
		clusterName = kindConfig.Name
	}

	// List Kind nodes (containers with label "io.x-k8s.kind.cluster=<name>")
	nodes, err := listKindNodes(ctx, dockerClient, clusterName)
	if err != nil {
		return fmt.Errorf("failed to list Kind nodes: %w", err)
	}

	if len(nodes) == 0 {
		return fmt.Errorf("no Kind nodes found for cluster %s", clusterName)
	}

	// Inject hosts.toml files into each node (only for non-mounted hosts)
	for _, entry := range entriesToInject {
		hostsTomlContent := registry.GenerateHostsToml(entry)

		for _, node := range nodes {
			err := injectHostsToml(ctx, dockerClient, node, entry.Host, hostsTomlContent)
			if err != nil {
				return fmt.Errorf("failed to inject hosts.toml for %s into node %s: %w", entry.Host, node, err)
			}
		}
	}

	return nil
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
			if strings.HasPrefix(mount.ContainerPath, certsPrefix) {
				// Extract the registry host from the path
				// e.g., /etc/containerd/certs.d/docker.io -> docker.io
				host := strings.TrimPrefix(mount.ContainerPath, certsPrefix)
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
func listKindNodes(ctx context.Context, dockerClient client.APIClient, clusterName string) ([]string, error) {
	containers, err := dockerClient.ContainerList(ctx, container.ListOptions{
		All: true,
	})
	if err != nil {
		return nil, err
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

// injectHostsToml creates the hosts directory and writes the hosts.toml file inside a Kind node.
func injectHostsToml(
	ctx context.Context,
	dockerClient client.APIClient,
	nodeName string,
	registryHost string,
	hostsTomlContent string,
) error {
	// Create the directory structure: /etc/containerd/certs.d/<registry-host>/
	certsDir := fmt.Sprintf("/etc/containerd/certs.d/%s", registryHost)

	// Execute: mkdir -p <dir> && cat > <dir>/hosts.toml
	// We use a shell command to create the directory and write the file in one go
	cmd := []string{
		"sh", "-c",
		fmt.Sprintf("mkdir -p %s && cat > %s/hosts.toml << 'HOSTS_TOML_EOF'\n%s\nHOSTS_TOML_EOF",
			certsDir, certsDir, hostsTomlContent),
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

	// Read and discard output
	var stdout, stderr bytes.Buffer
	_, _ = stdcopy.StdCopy(&stdout, &stderr, resp.Reader)

	// Check exit code
	inspectResp, err := dockerClient.ContainerExecInspect(ctx, execID.ID)
	if err != nil {
		return fmt.Errorf("failed to inspect exec: %w", err)
	}

	if inspectResp.ExitCode != 0 {
		return fmt.Errorf("exec failed with exit code %d: %s", inspectResp.ExitCode, stderr.String())
	}

	return nil
}

// prepareKindRegistryManager is a helper that prepares the registry manager and registry infos
// for Kind registry operations. Returns nil manager if mirrorSpecs is empty.
func prepareKindRegistryManager(
	ctx context.Context,
	mirrorSpecs []registry.MirrorSpec,
	dockerClient client.APIClient,
) (*dockerclient.RegistryManager, []registry.Info, error) {
	if len(mirrorSpecs) == 0 {
		return nil, nil, nil
	}

	upstreams := registry.BuildUpstreamLookup(mirrorSpecs)

	return registry.PrepareRegistryManager(
		ctx,
		dockerClient,
		func(usedPorts map[int]struct{}) []registry.Info {
			return buildRegistryInfosFromSpecs(mirrorSpecs, upstreams, usedPorts)
		},
	)
}

// SetupRegistries creates mirror registries based on mirror specifications.
// Registries are created without network attachment first, as the "kind" network
// doesn't exist until after the cluster is created. mirrorSpecs should contain the
// user-supplied mirror definitions so upstream URLs can be preserved when creating
// local proxy registry.
func SetupRegistries(
	ctx context.Context,
	kindConfig *v1alpha4.Cluster,
	clusterName string,
	dockerClient client.APIClient,
	mirrorSpecs []registry.MirrorSpec,
	writer io.Writer,
) error {
	registryMgr, registriesInfo, err := prepareKindRegistryManager(ctx, mirrorSpecs, dockerClient)
	if err != nil {
		return fmt.Errorf("failed to prepare kind registry manager: %w", err)
	}

	if registryMgr == nil {
		return nil
	}

	errSetup := registry.SetupRegistries(
		ctx,
		registryMgr,
		registriesInfo,
		clusterName,
		kindNetworkName,
		writer,
	)
	if errSetup != nil {
		return fmt.Errorf("failed to setup kind registries: %w", errSetup)
	}

	return nil
}

// buildRegistryInfosFromSpecs builds registry info from mirror specs directly.
func buildRegistryInfosFromSpecs(
	mirrorSpecs []registry.MirrorSpec,
	upstreams map[string]string,
	baseUsedPorts map[int]struct{},
) []registry.Info {
	var registryInfos []registry.Info

	usedPorts, nextPort := registry.InitPortAllocation(baseUsedPorts)

	for _, spec := range mirrorSpecs {
		host := strings.TrimSpace(spec.Host)
		if host == "" {
			continue
		}

		// Build endpoint for this host
		port := registry.AllocatePort(&nextPort, usedPorts)
		endpoint := fmt.Sprintf("http://%s:%d", host, port)

		// Get upstream URL
		upstream := spec.Remote
		if upstream == "" {
			upstream = registry.GenerateUpstreamURL(host)
		}
		if upstreams != nil && upstreams[host] != "" {
			upstream = upstreams[host]
		}

		info := registry.BuildRegistryInfo(host, []string{endpoint}, port, "", upstream)
		registryInfos = append(registryInfos, info)
	}

	return registryInfos
}

// ConnectRegistriesToNetwork connects existing registries to the Kind network.
// This should be called after the Kind cluster is created and the "kind" network exists.
func ConnectRegistriesToNetwork(
	ctx context.Context,
	mirrorSpecs []registry.MirrorSpec,
	dockerClient client.APIClient,
	writer io.Writer,
) error {
	if len(mirrorSpecs) == 0 {
		return nil
	}

	registriesInfo := buildRegistryInfosFromSpecs(mirrorSpecs, nil, nil)
	if len(registriesInfo) == 0 {
		return nil
	}

	errConnect := registry.ConnectRegistriesToNetwork(
		ctx,
		dockerClient,
		registriesInfo,
		kindNetworkName,
		writer,
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
	registryMgr, registriesInfo, err := prepareKindRegistryManager(ctx, mirrorSpecs, dockerClient)
	if err != nil {
		return fmt.Errorf("failed to prepare registry manager for cleanup: %w", err)
	}

	if registryMgr == nil {
		return nil
	}

	errCleanup := registry.CleanupRegistries(
		ctx,
		registryMgr,
		registriesInfo,
		clusterName,
		deleteVolumes,
		kindNetworkName,
		nil,
	)
	if errCleanup != nil {
		return fmt.Errorf("failed to cleanup kind registries: %w", errCleanup)
	}

	return nil
}
