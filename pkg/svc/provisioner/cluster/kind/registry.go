package kindprovisioner

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/registry"
	"github.com/docker/docker/client"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

const kindNetworkName = "kind"

// SetupRegistryHostsDirectory creates the containerd hosts directory structure for registry mirrors.
// This uses the modern hosts directory pattern instead of deprecated ContainerdConfigPatches.
// If a hosts.toml file already exists (from scaffolding), it is preserved.
// Returns the hosts directory manager and any error encountered.
func SetupRegistryHostsDirectory(
	mirrorSpecs []registry.MirrorSpec,
	clusterName string,
) (*registry.HostsDirectoryManager, error) {
	if len(mirrorSpecs) == 0 {
		return nil, nil
	}

	// Create hosts directory in current working directory for declarative configuration
	// Users can inspect, modify, and share these files as needed
	hostsDir := "kind-mirrors"
	mgr, err := registry.NewHostsDirectoryManager(hostsDir)
	if err != nil {
		return nil, fmt.Errorf("failed to create hosts directory manager: %w", err)
	}

	// Build mirror entries for any hosts that don't already have scaffolded files
	entries := registry.BuildMirrorEntries(mirrorSpecs, "", nil, nil, nil)
	if len(entries) == 0 {
		return nil, nil
	}

	// Only write hosts.toml files for registries that don't already have one
	for _, entry := range entries {
		hostsPath := filepath.Join(hostsDir, entry.Host, "hosts.toml")
		if _, statErr := os.Stat(hostsPath); statErr == nil {
			// File exists from scaffolding, preserve it
			continue
		}
		// File doesn't exist, write it
		if _, writeErr := mgr.WriteHostsToml(entry); writeErr != nil {
			_ = mgr.Cleanup()
			return nil, fmt.Errorf("failed to write hosts.toml for %s: %w", entry.Host, writeErr)
		}
	}

	return mgr, nil
}

// ConfigureKindWithHostsDirectory adds extraMounts to Kind nodes to mount the hosts directory.
// This configures containerd to use the modern hosts directory pattern for registry mirrors.
func ConfigureKindWithHostsDirectory(
	kindConfig *v1alpha4.Cluster,
	hostsDir string,
	mirrorSpecs []registry.MirrorSpec,
) error {
	if kindConfig == nil || hostsDir == "" || len(mirrorSpecs) == 0 {
		return nil
	}

	// Ensure nodes exist
	if len(kindConfig.Nodes) == 0 {
		// Add default control-plane node if none exist
		kindConfig.Nodes = []v1alpha4.Node{
			{
				Role: v1alpha4.ControlPlaneRole,
			},
		}
	}

	// Add extraMounts for each registry host
	for _, spec := range mirrorSpecs {
		host := strings.TrimSpace(spec.Host)
		if host == "" {
			continue
		}

		// Each registry gets its own directory mounted to /etc/containerd/certs.d/<host>
		hostPath := filepath.Join(hostsDir, host)
		containerPath := fmt.Sprintf("/etc/containerd/certs.d/%s", host)

		mount := v1alpha4.Mount{
			HostPath:      hostPath,
			ContainerPath: containerPath,
			Readonly:      true,
		}

		// Add mount to all nodes
		for i := range kindConfig.Nodes {
			kindConfig.Nodes[i].ExtraMounts = append(kindConfig.Nodes[i].ExtraMounts, mount)
		}
	}

	return nil
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
	if len(mirrorSpecs) == 0 {
		return nil
	}

	upstreams := registry.BuildUpstreamLookup(mirrorSpecs)

	registryMgr, registriesInfo, err := registry.PrepareRegistryManager(
		ctx,
		dockerClient,
		func(usedPorts map[int]struct{}) []registry.Info {
			return buildRegistryInfosFromSpecs(mirrorSpecs, upstreams, usedPorts)
		},
	)
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
	if len(mirrorSpecs) == 0 {
		// Also cleanup hosts directory
		CleanupHostsDirectory(clusterName)
		return nil
	}

	upstreams := registry.BuildUpstreamLookup(mirrorSpecs)

	registryMgr, registriesInfo, err := registry.PrepareRegistryManager(
		ctx,
		dockerClient,
		func(usedPorts map[int]struct{}) []registry.Info {
			return buildRegistryInfosFromSpecs(mirrorSpecs, upstreams, usedPorts)
		},
	)
	if err != nil {
		return fmt.Errorf("failed to prepare registry manager for cleanup: %w", err)
	}

	if registryMgr == nil {
		// Still cleanup hosts directory
		CleanupHostsDirectory(clusterName)
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

	// Cleanup hosts directory if it exists
	CleanupHostsDirectory(clusterName)

	return nil
}

// CleanupHostsDirectory removes the hosts directory created for the cluster.
// This is best-effort cleanup and does not return errors.
func CleanupHostsDirectory(clusterName string) {
	hostsDir := "kind-mirrors"
	_ = os.RemoveAll(hostsDir)
}
