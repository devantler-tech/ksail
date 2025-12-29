package kindprovisioner

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"strings"

	registryutil "github.com/devantler-tech/ksail/v5/pkg/registry"

	dockerclient "github.com/devantler-tech/ksail/v5/pkg/client/docker"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/registry"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

const kindNetworkName = "kind"

// randomDelimiterBytes is the number of random bytes used to generate heredoc delimiters.
// 8 bytes produces 16 hex characters, making collisions with user content extremely unlikely.
const randomDelimiterBytes = 8

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
	mirrorSpecs []registryutil.MirrorSpec,
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

	return injectHostsTomlIntoNodes(ctx, dockerClient, nodes, entriesToInject)
}

// getEntriesToInject returns mirror entries that need to be injected into Kind nodes.
// It filters out entries that already have extraMounts configured in the Kind config.
func getEntriesToInject(
	kindConfig *v1alpha4.Cluster,
	mirrorSpecs []registryutil.MirrorSpec,
) []registryutil.MirrorEntry {
	if len(mirrorSpecs) == 0 {
		return nil
	}

	mountedHosts := buildMountedHostsSet(kindConfig)
	entries := registryutil.BuildMirrorEntries(mirrorSpecs, "", nil, nil, nil)

	var entriesToInject []registryutil.MirrorEntry

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

// injectHostsTomlIntoNodes injects hosts.toml files into all Kind nodes for the given entries.
func injectHostsTomlIntoNodes(
	ctx context.Context,
	dockerClient client.APIClient,
	nodes []string,
	entries []registryutil.MirrorEntry,
) error {
	for _, entry := range entries {
		hostsTomlContent := registryutil.GenerateHostsToml(entry)

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

// generateRandomDelimiter creates a random heredoc delimiter to prevent injection attacks.
// The delimiter is prefixed with "EOF_" and followed by 16 random hex characters,
// making it extremely unlikely to appear in user-controlled content.
func generateRandomDelimiter() (string, error) {
	randomBytes := make([]byte, randomDelimiterBytes)

	_, err := rand.Read(randomBytes)
	if err != nil {
		return "", fmt.Errorf("failed to generate random delimiter: %w", err)
	}

	return "EOF_" + hex.EncodeToString(randomBytes), nil
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
	certsDir := "/etc/containerd/certs.d/" + registryHost

	// Escape the directory path for safe use in shell commands
	escapedCertsDir := EscapeShellArg(certsDir)

	// Generate a random heredoc delimiter to prevent injection attacks
	delimiter, err := generateRandomDelimiter()
	if err != nil {
		return err
	}

	// Execute: mkdir -p <dir> && cat > <dir>/hosts.toml
	// We use a shell command to create the directory and write the file in one go
	// The heredoc delimiter is randomized to prevent content injection attacks
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

	// Read and discard output
	var stdout, stderr bytes.Buffer

	_, _ = stdcopy.StdCopy(&stdout, &stderr, resp.Reader)

	// Check exit code
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

// EscapeShellArg escapes a string for safe use in POSIX shell commands.
// It wraps the string in single quotes and escapes any single quotes within.
func EscapeShellArg(arg string) string {
	// Replace ' with '\'' (end quote, escaped quote, start quote)
	escaped := strings.ReplaceAll(arg, "'", "'\\''")

	return "'" + escaped + "'"
}

// prepareKindRegistryManager is a helper that prepares the registry manager and registry infos
// for Kind registry operations. Returns nil manager if mirrorSpecs is empty.
func prepareKindRegistryManager(
	ctx context.Context,
	mirrorSpecs []registryutil.MirrorSpec,
	dockerClient client.APIClient,
) (*dockerclient.RegistryManager, []registry.Info, error) {
	if len(mirrorSpecs) == 0 {
		return nil, nil, nil
	}

	upstreams := registryutil.BuildUpstreamLookup(mirrorSpecs)

	registryMgr, infos, err := registry.PrepareRegistryManager(
		ctx,
		dockerClient,
		func(usedPorts map[int]struct{}) []registry.Info {
			return registry.BuildRegistryInfosFromSpecs(mirrorSpecs, upstreams, usedPorts)
		},
	)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to prepare registry manager: %w", err)
	}

	return registryMgr, infos, nil
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
	mirrorSpecs []registryutil.MirrorSpec,
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

// ConnectRegistriesToNetwork connects existing registries to the Kind network.
// This should be called after the Kind cluster is created and the "kind" network exists.
func ConnectRegistriesToNetwork(
	ctx context.Context,
	mirrorSpecs []registryutil.MirrorSpec,
	dockerClient client.APIClient,
	writer io.Writer,
) error {
	if len(mirrorSpecs) == 0 {
		return nil
	}

	registriesInfo := registry.BuildRegistryInfosFromSpecs(mirrorSpecs, nil, nil)
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
	mirrorSpecs []registryutil.MirrorSpec,
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
