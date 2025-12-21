package kindprovisioner

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	dockerclient "github.com/devantler-tech/ksail/v5/pkg/client/docker"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/registry"
	"github.com/docker/docker/client"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

const kindNetworkName = "kind"

// SetupRegistryHostsDirectory creates the containerd hosts directory structure for registry mirrors.
// This uses the modern hosts directory pattern instead of deprecated ContainerdConfigPatches.
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
	hostsDir := "kind-mirror-config"
	mgr, err := registry.NewHostsDirectoryManager(hostsDir)
	if err != nil {
		return nil, fmt.Errorf("failed to create hosts directory manager: %w", err)
	}

	// Build mirror entries
	entries := registry.BuildMirrorEntries(mirrorSpecs, "", nil, nil, nil)
	if len(entries) == 0 {
		return nil, nil
	}

	// Write all hosts.toml files
	_, err = mgr.WriteAllHostsToml(entries)
	if err != nil {
		_ = mgr.Cleanup() // Try to cleanup on error
		return nil, fmt.Errorf("failed to write hosts.toml files: %w", err)
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

// setupRegistryManager creates a registry manager and extracts registries from Kind config.
// Returns nil if no setup is needed.
func setupRegistryManager(
	ctx context.Context,
	kindConfig *v1alpha4.Cluster,
	dockerClient client.APIClient,
	upstreams map[string]string,
) (*dockerclient.RegistryManager, []registry.Info, error) {
	if kindConfig == nil {
		return nil, nil, nil
	}

	registryMgr, infos, err := registry.PrepareRegistryManager(
		ctx,
		dockerClient,
		func(usedPorts map[int]struct{}) []registry.Info {
			return extractRegistriesFromKind(kindConfig, upstreams, usedPorts)
		},
	)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to prepare kind registry manager: %w", err)
	}

	return registryMgr, infos, nil
}

// SetupRegistries creates mirror registries based on Kind cluster configuration.
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
	upstreams := registry.BuildUpstreamLookup(mirrorSpecs)

	registryMgr, registriesInfo, err := setupRegistryManager(
		ctx,
		kindConfig,
		dockerClient,
		upstreams,
	)
	if err != nil {
		return err
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
	kindConfig *v1alpha4.Cluster,
	dockerClient client.APIClient,
	writer io.Writer,
) error {
	if kindConfig == nil {
		return nil
	}

	registriesInfo := extractRegistriesFromKind(kindConfig, nil, nil)
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
	kindConfig *v1alpha4.Cluster,
	clusterName string,
	dockerClient client.APIClient,
	deleteVolumes bool,
) error {
	registryMgr, registriesInfo, err := setupRegistryManager(ctx, kindConfig, dockerClient, nil)
	if err != nil {
		return err
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

	// Cleanup hosts directory if it exists
	CleanupHostsDirectory(clusterName)

	return nil
}

// CleanupHostsDirectory removes the hosts directory created for the cluster.
// This is best-effort cleanup and does not return errors.
func CleanupHostsDirectory(clusterName string) {
	hostsDir := "kind-mirror-config"
	_ = os.RemoveAll(hostsDir)
}

// ExtractRegistriesFromKindForTesting extracts registry information from Kind configuration.
// This function is exported for testing purposes.
func ExtractRegistriesFromKindForTesting(
	kindConfig *v1alpha4.Cluster,
	upstreams map[string]string,
) []registry.Info {
	return extractRegistriesFromKind(kindConfig, upstreams, nil)
}

// extractRegistriesFromKind is the internal implementation.
func extractRegistriesFromKind(
	kindConfig *v1alpha4.Cluster,
	upstreams map[string]string,
	baseUsedPorts map[int]struct{},
) []registry.Info {
	if kindConfig == nil {
		return nil
	}

	var registryInfos []registry.Info

	seenHosts := make(map[string]bool)
	usedPorts, nextPort := registry.InitPortAllocation(baseUsedPorts)

	for _, patch := range kindConfig.ContainerdConfigPatches {
		mirrors := parseContainerdConfig(patch)
		if len(mirrors) == 0 {
			continue
		}

		hosts := make([]string, 0, len(mirrors))
		for host := range mirrors {
			hosts = append(hosts, host)
		}

		registry.SortHosts(hosts)

		for _, host := range hosts {
			if seenHosts[host] {
				continue
			}

			seenHosts[host] = true

			endpoints := mirrors[host]
			port := registry.ExtractRegistryPort(endpoints, usedPorts, &nextPort)
			info := registry.BuildRegistryInfo(host, endpoints, port, "", upstreams[host])
			registryInfos = append(registryInfos, info)
		}
	}

	return registryInfos
}

// parseContainerdConfig is the internal implementation.
func parseContainerdConfig(patch string) map[string][]string {
	mirrors := make(map[string][]string)
	lines := strings.Split(patch, "\n")

	var currentHost string

	var inEndpointArray bool

	var currentEndpoints []string

	for i := range lines {
		line := strings.TrimSpace(lines[i])

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Process registry mirror section headers
		if strings.Contains(line, `registry.mirrors."`) {
			currentHost = processHostMirrorsLine(
				line,
				currentHost,
				currentEndpoints,
				mirrors,
			)
			currentEndpoints = nil
			inEndpointArray = false

			continue
		}

		// Process endpoint configuration lines
		if currentHost != "" && strings.Contains(line, "endpoint") {
			currentHost, currentEndpoints, inEndpointArray = processEndpointLine(
				line,
				currentHost,
				currentEndpoints,
				mirrors,
			)

			continue
		}

		// Process multiline array elements
		if inEndpointArray {
			currentHost, currentEndpoints, inEndpointArray = processMultilineArrayLine(
				line,
				currentHost,
				currentEndpoints,
				mirrors,
			)
		}
	}

	// Save any remaining host endpoints
	saveHostEndpoints(currentHost, currentEndpoints, mirrors)

	return mirrors
}

// processHostMirrorsLine processes a registry mirrors section header line.
// Returns updated currentHost.
func processHostMirrorsLine(
	line string,
	currentHost string,
	currentEndpoints []string,
	mirrors map[string][]string,
) string {
	// Save previous host's endpoints if any
	saveHostEndpoints(currentHost, currentEndpoints, mirrors)

	// Extract new host from line like: [plugins."io.containerd.grpc.v1.cri".registry.mirrors."docker.io"]
	start := strings.Index(line, `mirrors."`)
	if start < 0 {
		return ""
	}

	start += len(`mirrors."`)

	end := strings.Index(line[start:], `"`)
	if end <= 0 {
		return ""
	}

	newHost := line[start : start+end]

	// Skip if this host is already in mirrors (preserve first occurrence)
	if _, exists := mirrors[newHost]; exists {
		return ""
	}

	return newHost
}

// processEndpointLine processes an endpoint configuration line.
// Returns updated currentHost, currentEndpoints, and inEndpointArray.
func processEndpointLine(
	line string,
	currentHost string,
	currentEndpoints []string,
	mirrors map[string][]string,
) (string, []string, bool) {
	// Check for inline array format: endpoint = ["http://..."]
	cleanLine := strings.ReplaceAll(line, " ", "")
	if strings.Contains(cleanLine, `["`) && strings.Contains(cleanLine, `"]`) {
		endpoints := extractEndpointsFromInlineArray(line)
		currentEndpoints = append(currentEndpoints, endpoints...)
		// Save immediately for inline format
		saveHostEndpoints(currentHost, currentEndpoints, mirrors)

		return "", nil, false
	}

	// Check for multiline array start: endpoint = [
	if strings.Contains(cleanLine, "[") && !strings.Contains(cleanLine, "]") {
		return currentHost, currentEndpoints, true
	}

	return currentHost, currentEndpoints, false
}

// processMultilineArrayLine processes a line within a multiline endpoint array.
// Returns updated currentHost, currentEndpoints, and inEndpointArray.
func processMultilineArrayLine(
	line string,
	currentHost string,
	currentEndpoints []string,
	mirrors map[string][]string,
) (string, []string, bool) {
	if strings.Contains(line, "]") {
		// End of array - extract any endpoint on the closing line
		if endpoint := extractEndpointFromLine(line); endpoint != "" {
			currentEndpoints = append(currentEndpoints, endpoint)
		}
		// Save collected endpoints
		saveHostEndpoints(currentHost, currentEndpoints, mirrors)

		return "", nil, false
	}

	// Extract endpoint from array element line
	if endpoint := extractEndpointFromLine(line); endpoint != "" {
		currentEndpoints = append(currentEndpoints, endpoint)
	}

	return currentHost, currentEndpoints, true
}

// saveHostEndpoints saves endpoints for a host to the mirrors map.
func saveHostEndpoints(host string, endpoints []string, mirrors map[string][]string) {
	if host != "" && len(endpoints) > 0 {
		mirrors[host] = endpoints
	}
}

// extractEndpointsFromInlineArray extracts all endpoints from an inline array format.
// Example: endpoint = ["http://localhost:5000", "http://localhost:5001"]
func extractEndpointsFromInlineArray(line string) []string {
	var endpoints []string

	// Find the array content between [ and ]
	start := strings.Index(line, "[")

	end := strings.LastIndex(line, "]")
	if start < 0 || end < 0 || start >= end {
		return endpoints
	}

	arrayContent := line[start+1 : end]

	// Split by comma and extract quoted strings
	parts := strings.SplitSeq(arrayContent, ",")
	for part := range parts {
		part = strings.TrimSpace(part)
		if endpoint := extractQuotedString(part); endpoint != "" {
			endpoints = append(endpoints, endpoint)
		}
	}

	return endpoints
}

// extractEndpointFromLine extracts an endpoint URL from a line.
// Handles quoted strings with surrounding whitespace.
func extractEndpointFromLine(line string) string {
	line = strings.TrimSpace(line)
	// Remove trailing comma if present
	line = strings.TrimSuffix(line, ",")

	return extractQuotedString(line)
}

// extractQuotedString is the internal implementation.
func extractQuotedString(str string) string {
	str = strings.TrimSpace(str)

	// Find first and last quote
	firstQuote := strings.Index(str, `"`)
	if firstQuote < 0 {
		return ""
	}

	lastQuote := strings.LastIndex(str, `"`)
	if lastQuote <= firstQuote {
		return ""
	}

	return str[firstQuote+1 : lastQuote]
}
