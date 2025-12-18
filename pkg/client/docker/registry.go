package docker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
)

// Registry error definitions.
var (
	// ErrRegistryNotFound is returned when a registry container is not found.
	ErrRegistryNotFound = errors.New("registry not found")
	// ErrRegistryAlreadyExists is returned when trying to create a registry that already exists.
	ErrRegistryAlreadyExists = errors.New("registry already exists")
	// ErrRegistryPortNotFound is returned when the registry port cannot be determined.
	ErrRegistryPortNotFound = errors.New("registry port not found")
)

const (
	// Registry image configuration.

	// RegistryImageName is the default registry image to use.
	RegistryImageName = "registry:3"

	// Registry labeling and identification.

	// RegistryLabelKey marks registry containers as managed by ksail.
	RegistryLabelKey = "io.ksail.registry"

	// Registry port configuration.

	// DefaultRegistryPort is the default port for registry containers.
	DefaultRegistryPort = 5000
	// RegistryPortBase is the base port number for calculating registry ports.
	RegistryPortBase = 5000
	// HostPortParts is the expected number of parts in a host:port string.
	HostPortParts = 2
	// RegistryContainerPort is the internal port exposed by the registry container.
	RegistryContainerPort = "5000/tcp"
	// RegistryHostIP is the host IP address to bind registry ports to.
	RegistryHostIP = "127.0.0.1"

	// Registry container configuration.

	// RegistryDataPath is the path inside the container where registry data is stored.
	RegistryDataPath = "/var/lib/registry"
	// RegistryRestartPolicy defines the container restart policy.
	RegistryRestartPolicy = "unless-stopped"

	// RegistryConfigTemplate is the base configuration template for registry containers.
	RegistryConfigTemplate = `version: 0.1
log:
  level: info
  fields:
    service: registry
storage:
  cache:
    blobdescriptor: inmemory
  filesystem:
    rootdirectory: /var/lib/registry
  delete:
    enabled: true
http:
  addr: :5000
health:
  storagedriver:
    enabled: true
    interval: 10s
    threshold: 3`
)

// RegistryManager manages Docker registry containers for mirror/pull-through caching.
type RegistryManager struct {
	client client.APIClient
}

// NewRegistryManager creates a new RegistryManager.
func NewRegistryManager(apiClient client.APIClient) (*RegistryManager, error) {
	if apiClient == nil {
		return nil, ErrAPIClientNil
	}

	return &RegistryManager{
		client: apiClient,
	}, nil
}

// RegistryConfig holds configuration for creating a registry.
type RegistryConfig struct {
	Name        string
	Port        int
	UpstreamURL string
	ClusterName string
	NetworkName string
	VolumeName  string
}

// CreateRegistry creates a registry container with the given configuration.
// If the registry already exists, it returns ErrRegistryAlreadyExists.
func (rm *RegistryManager) CreateRegistry(ctx context.Context, config RegistryConfig) error {
	// Check if registry already exists
	exists, err := rm.registryExists(ctx, config.Name)
	if err != nil {
		return fmt.Errorf("failed to check if registry exists: %w", err)
	}

	if exists {
		// Add cluster label to existing registry
		return rm.addClusterLabel(ctx, config.Name, config.ClusterName)
	}

	// Pull registry image if not present
	err = rm.ensureRegistryImage(ctx)
	if err != nil {
		return fmt.Errorf("failed to ensure registry image: %w", err)
	}

	// Prepare registry resources (volume and config file)
	volumeName, configFilePath, err := rm.prepareRegistryResources(ctx, config)
	if err != nil {
		return fmt.Errorf("failed to prepare registry resources: %w", err)
	}

	// Clean up config file when done
	if configFilePath != "" {
		defer func() {
			_ = os.Remove(configFilePath)
		}()
	}

	// Create and start the container
	return rm.createAndStartContainer(ctx, config, volumeName, configFilePath)
}

// DeleteRegistry removes a registry container and optionally its volume.
// If deleteVolume is true, the associated volume will be removed.
// If the registry is still in use by other clusters, it returns an error.
func (rm *RegistryManager) DeleteRegistry(
	ctx context.Context,
	name, _ string,
	deleteVolume bool,
	networkName string,
	volumeName string,
) error {
	containers, err := rm.listRegistryContainers(ctx, name)
	if err != nil {
		return fmt.Errorf("failed to list registry containers: %w", err)
	}

	if len(containers) == 0 {
		if deleteVolume {
			orphanErr := cleanupOrphanedRegistryVolume(ctx, rm.client, volumeName, name)
			if orphanErr != nil {
				return orphanErr
			}
		}

		return ErrRegistryNotFound
	}

	registryContainer := containers[0]

	trimmedNetwork := strings.TrimSpace(networkName)

	inspect, err := inspectContainer(ctx, rm.client, registryContainer.ID)
	if err != nil {
		return err
	}

	inspect, err = disconnectRegistryNetwork(
		ctx,
		rm.client,
		registryContainer.ID,
		name,
		trimmedNetwork,
		inspect,
	)
	if err != nil {
		return err
	}

	if registryAttachedToOtherClusters(inspect, trimmedNetwork) {
		return nil
	}

	stopErr := rm.stopRegistryContainer(ctx, registryContainer)
	if stopErr != nil {
		return stopErr
	}

	removeErr := rm.client.ContainerRemove(ctx, registryContainer.ID, container.RemoveOptions{})
	if removeErr != nil {
		return fmt.Errorf("failed to remove registry container: %w", removeErr)
	}

	return cleanupRegistryVolume(ctx, rm.client, registryContainer, volumeName, name, deleteVolume)
}

// ListRegistries returns a list of all ksail registry containers.
func (rm *RegistryManager) ListRegistries(ctx context.Context) ([]string, error) {
	containers, err := rm.listAllRegistryContainers(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list registry containers: %w", err)
	}

	registries := make([]string, 0, len(containers))

	seen := make(map[string]struct{}, len(containers))
	for _, c := range containers {
		name := c.Labels[RegistryLabelKey]
		if name == "" {
			for _, rawName := range c.Names {
				trimmed := strings.TrimPrefix(rawName, "/")
				if trimmed != "" {
					name = trimmed

					break
				}
			}
		}

		if name == "" {
			continue
		}

		if _, exists := seen[name]; exists {
			continue
		}

		seen[name] = struct{}{}
		registries = append(registries, name)
	}

	return registries, nil
}

// IsRegistryInUse checks if a registry is being used by any clusters.
// A registry is considered in use if it exists and is running.
func (rm *RegistryManager) IsRegistryInUse(ctx context.Context, name string) (bool, error) {
	containers, err := rm.listRegistryContainers(ctx, name)
	if err != nil {
		return false, fmt.Errorf("failed to list registry containers: %w", err)
	}

	if len(containers) == 0 {
		return false, nil
	}

	// Check if container is running
	return containers[0].State == "running", nil
}

// GetRegistryPort returns the host port for a registry.
func (rm *RegistryManager) GetRegistryPort(ctx context.Context, name string) (int, error) {
	containers, err := rm.listRegistryContainers(ctx, name)
	if err != nil {
		return 0, fmt.Errorf("failed to list registry containers: %w", err)
	}

	if len(containers) == 0 {
		return 0, ErrRegistryNotFound
	}

	// Get port from container ports
	for _, port := range containers[0].Ports {
		if port.PrivatePort == DefaultRegistryPort {
			return int(port.PublicPort), nil
		}
	}

	return 0, ErrRegistryPortNotFound
}

// prepareRegistryResources creates the volume and config file for a registry.
func (rm *RegistryManager) prepareRegistryResources(
	ctx context.Context,
	config RegistryConfig,
) (string, string, error) {
	// Create volume for registry data using a distribution-agnostic name for reuse
	volumeName := rm.resolveVolumeName(config)
	if volumeName == "" {
		volumeName = config.Name
	}

	err := rm.createVolume(ctx, volumeName)
	if err != nil {
		return "", "", fmt.Errorf("failed to create registry volume: %w", err)
	}

	// Create config file if upstream URL is provided
	var configFilePath string
	if config.UpstreamURL != "" {
		configFilePath, err = rm.createRegistryConfigFile(config.Name, config.UpstreamURL)
		if err != nil {
			// Clean up the volume we just created since config file creation failed
			_ = rm.client.VolumeRemove(ctx, volumeName, false)

			return "", "", fmt.Errorf("failed to create registry config file: %w", err)
		}
	}

	return volumeName, configFilePath, nil
}

// createAndStartContainer creates and starts a registry container.
func (rm *RegistryManager) createAndStartContainer(
	ctx context.Context,
	config RegistryConfig,
	volumeName, configFilePath string,
) error {
	// Prepare container configuration
	containerConfig := rm.buildContainerConfig(config)
	hostConfig := rm.buildHostConfig(config, volumeName, configFilePath)
	networkConfig := rm.buildNetworkConfig(config)

	// Create container
	resp, err := rm.client.ContainerCreate(
		ctx,
		containerConfig,
		hostConfig,
		networkConfig,
		nil,
		config.Name,
	)
	if err != nil {
		return fmt.Errorf("failed to create registry container: %w", err)
	}

	// Start container
	err = rm.client.ContainerStart(ctx, resp.ID, container.StartOptions{})
	if err != nil {
		return fmt.Errorf("failed to start registry container: %w", err)
	}

	return nil
}

// Container management helpers.

// registryExists checks if a registry container with the given name exists.
func (rm *RegistryManager) registryExists(ctx context.Context, name string) (bool, error) {
	containers, err := rm.listRegistryContainers(ctx, name)
	if err != nil {
		return false, err
	}

	return len(containers) > 0, nil
}

// listRegistryContainers lists all containers matching the given registry name.
func (rm *RegistryManager) listRegistryContainers(
	ctx context.Context,
	name string,
) ([]container.Summary, error) {
	filterArgs := filters.NewArgs()
	filterArgs.Add("name", name)
	filterArgs.Add("ancestor", RegistryImageName)
	filterArgs.Add("label", fmt.Sprintf("%s=%s", RegistryLabelKey, name))

	containers, err := rm.client.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filterArgs,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list registry containers: %w", err)
	}

	return containers, nil
}

// listAllRegistryContainers lists all ksail-managed registry containers.
func (rm *RegistryManager) listAllRegistryContainers(
	ctx context.Context,
) ([]container.Summary, error) {
	filterArgs := filters.NewArgs()
	filterArgs.Add("ancestor", RegistryImageName)
	filterArgs.Add("label", RegistryLabelKey)

	containers, err := rm.client.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filterArgs,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list all registry containers: %w", err)
	}

	return containers, nil
}

// ensureRegistryImage pulls the registry image if not already present locally.
func (rm *RegistryManager) ensureRegistryImage(ctx context.Context) error {
	// Check if image exists
	_, err := rm.client.ImageInspect(ctx, RegistryImageName)
	if err == nil {
		return nil
	}

	// Pull image
	reader, err := rm.client.ImagePull(ctx, RegistryImageName, image.PullOptions{})
	if err != nil {
		return fmt.Errorf("failed to pull registry image: %w", err)
	}

	// Consume pull output
	_, err = io.Copy(io.Discard, reader)
	closeErr := reader.Close()

	if err != nil {
		return fmt.Errorf("failed to read image pull output: %w", err)
	}

	if closeErr != nil {
		return fmt.Errorf("failed to close image pull reader: %w", closeErr)
	}

	return nil
}

// createVolume creates a Docker volume if it doesn't already exist.
func (rm *RegistryManager) createVolume(
	ctx context.Context,
	volumeName string,
) error {
	// Check if volume already exists
	_, err := rm.client.VolumeInspect(ctx, volumeName)
	if err == nil {
		return nil // Volume already exists
	}

	// Create volume
	_, err = rm.client.VolumeCreate(ctx, volume.CreateOptions{
		Name: volumeName,
	})
	if err != nil {
		return fmt.Errorf("failed to create volume: %w", err)
	}

	return nil
}

// Configuration builders.

// buildContainerConfig builds the container configuration for a registry.
func (rm *RegistryManager) buildContainerConfig(
	config RegistryConfig,
) *container.Config {
	labels := map[string]string{}
	if config.Name != "" {
		labels[RegistryLabelKey] = config.Name
	}

	return &container.Config{
		Image: RegistryImageName,
		ExposedPorts: nat.PortSet{
			RegistryContainerPort: struct{}{},
		},
		Labels: labels,
	}
}

// generateRegistryConfig creates a registry configuration with optional proxy settings.
func (rm *RegistryManager) generateRegistryConfig(upstreamURL string) string {
	baseConfig := RegistryConfigTemplate

	if upstreamURL != "" {
		baseConfig += "\nproxy:\n  remoteurl: " + upstreamURL
	}

	return baseConfig
}

// createRegistryConfigFile creates a temporary registry configuration file.
// The caller is responsible for deleting the returned file path when done.
// Deletion can be done with os.Remove() - if deletion fails, it will leave
// a temporary file in the system temp directory.
func (rm *RegistryManager) createRegistryConfigFile(
	registryName, upstreamURL string,
) (string, error) {
	configContent := rm.generateRegistryConfig(upstreamURL)

	// Create temp file
	tmpFile, err := os.CreateTemp("", "registry-config-"+registryName+"-*.yml")
	if err != nil {
		return "", fmt.Errorf("failed to create temp config file: %w", err)
	}

	defer func() {
		closeErr := tmpFile.Close()
		if closeErr != nil {
			fmt.Fprintf(
				os.Stderr,
				"warning: failed to close temp config file %s: %v\n",
				tmpFile.Name(),
				closeErr,
			)
		}
	}()

	// Write config content
	_, err = tmpFile.WriteString(configContent)
	if err != nil {
		_ = os.Remove(tmpFile.Name())

		return "", fmt.Errorf("failed to write config content: %w", err)
	}

	return tmpFile.Name(), nil
}

// buildHostConfig builds the host configuration including port bindings and mounts.
func (rm *RegistryManager) buildHostConfig(
	config RegistryConfig,
	volumeName string,
	configFilePath string,
) *container.HostConfig {
	portBindings := nat.PortMap{}
	if config.Port > 0 {
		portBindings[RegistryContainerPort] = []nat.PortBinding{
			{
				HostIP:   RegistryHostIP,
				HostPort: strconv.Itoa(config.Port),
			},
		}
	}

	mounts := []mount.Mount{
		{
			Type:   mount.TypeVolume,
			Source: volumeName,
			Target: RegistryDataPath,
		},
	}

	// If config file path is provided, mount it
	if configFilePath != "" {
		absPath, err := filepath.Abs(configFilePath)
		if err != nil {
			// Log structured error - this is a critical configuration issue
			// Registry will start but without proxy configuration, which defeats the purpose
			errMsg := "error: failed to resolve absolute path for config file %s: %v\n" +
				"warning: registry will start without proxy configuration\n"
			fmt.Fprintf(
				os.Stderr,
				errMsg,
				configFilePath,
				err,
			)
		} else {
			mounts = append(mounts, mount.Mount{
				Type:     mount.TypeBind,
				Source:   absPath,
				Target:   "/etc/distribution/config.yml",
				ReadOnly: true,
			})
		}
	}

	return &container.HostConfig{
		PortBindings: portBindings,
		RestartPolicy: container.RestartPolicy{
			Name: RegistryRestartPolicy,
		},
		Mounts: mounts,
	}
}

// buildNetworkConfig builds the network configuration for connecting to a cluster network.
func (rm *RegistryManager) buildNetworkConfig(config RegistryConfig) *network.NetworkingConfig {
	if config.NetworkName == "" {
		return nil
	}

	return &network.NetworkingConfig{
		EndpointsConfig: map[string]*network.EndpointSettings{
			config.NetworkName: {},
		},
	}
}

// resolveVolumeName determines the volume name to use for the registry.
func (rm *RegistryManager) resolveVolumeName(config RegistryConfig) string {
	if config.VolumeName != "" {
		return config.VolumeName
	}

	return NormalizeVolumeName(config.Name)
}

// NormalizeVolumeName trims registry names and removes distribution prefixes such as kind- or k3d-.
func NormalizeVolumeName(registryName string) string {
	trimmed := strings.TrimSpace(registryName)
	if trimmed == "" {
		return ""
	}

	if strings.HasPrefix(trimmed, "kind-") || strings.HasPrefix(trimmed, "k3d-") {
		if idx := strings.Index(trimmed, "-"); idx >= 0 && idx < len(trimmed)-1 {
			candidate := trimmed[idx+1:]
			if candidate != "" {
				return candidate
			}
		}
	}

	return trimmed
}

// addClusterLabel is a no-op with network-based tracking.
// Previously used for label-based tracking, now replaced by network connections.
// Kept for interface compatibility but may be removed in future refactoring.
func (rm *RegistryManager) addClusterLabel(
	_ context.Context,
	_, _ string,
) error {
	return nil
}

// stopRegistryContainer stops a registry container if it's running.
func (rm *RegistryManager) stopRegistryContainer(
	ctx context.Context,
	registry container.Summary,
) error {
	if !strings.EqualFold(registry.State, "running") {
		return nil
	}

	err := rm.client.ContainerStop(ctx, registry.ID, container.StopOptions{})
	if err != nil {
		return fmt.Errorf("failed to stop registry container: %w", err)
	}

	return nil
}

// Volume management helpers.

// deriveRegistryVolumeName extracts the volume name from a registry container.
func deriveRegistryVolumeName(registry container.Summary, fallback string) string {
	for _, mountPoint := range registry.Mounts {
		if mountPoint.Type == mount.TypeVolume && mountPoint.Name != "" {
			return mountPoint.Name
		}
	}

	if sanitized := NormalizeVolumeName(fallback); sanitized != "" {
		return sanitized
	}

	return strings.TrimSpace(fallback)
}

// Network management helpers.

// inspectContainer retrieves detailed information about a container.
func inspectContainer(
	ctx context.Context,
	dockerClient client.APIClient,
	containerID string,
) (container.InspectResponse, error) {
	inspect, err := dockerClient.ContainerInspect(ctx, containerID)
	if err != nil {
		return container.InspectResponse{}, fmt.Errorf(
			"failed to inspect registry container: %w",
			err,
		)
	}

	return inspect, nil
}

// disconnectRegistryNetwork disconnects a registry from a network and returns updated inspection data.
func disconnectRegistryNetwork(
	ctx context.Context,
	dockerClient client.APIClient,
	containerID string,
	name string,
	network string,
	inspect container.InspectResponse,
) (container.InspectResponse, error) {
	if network == "" {
		return inspect, nil
	}

	err := dockerClient.NetworkDisconnect(ctx, network, containerID, true)
	if err != nil && !cerrdefs.IsNotFound(err) {
		return container.InspectResponse{}, fmt.Errorf(
			"failed to disconnect registry %s from network %s: %w",
			name,
			network,
			err,
		)
	}

	return inspectContainer(ctx, dockerClient, containerID)
}

// Cleanup helpers.

// cleanupRegistryVolume removes a registry's volume if deletion is requested.
func cleanupRegistryVolume(
	ctx context.Context,
	dockerClient client.APIClient,
	registryContainer container.Summary,
	explicitVolume string,
	fallbackName string,
	deleteVolume bool,
) error {
	if !deleteVolume {
		return nil
	}

	volumeCandidate := strings.TrimSpace(explicitVolume)
	if volumeCandidate == "" {
		volumeCandidate = deriveRegistryVolumeName(registryContainer, fallbackName)
	}

	_, err := removeRegistryVolume(ctx, dockerClient, volumeCandidate)

	return err
}

// cleanupOrphanedRegistryVolume attempts to remove orphaned registry volumes.
func cleanupOrphanedRegistryVolume(
	ctx context.Context,
	dockerClient client.APIClient,
	explicitVolume string,
	fallbackName string,
) error {
	candidates := uniqueNonEmpty(
		explicitVolume,
		NormalizeVolumeName(fallbackName),
		fallbackName,
	)

	for _, candidate := range candidates {
		removed, err := removeRegistryVolume(ctx, dockerClient, candidate)
		if err != nil {
			return err
		}

		if removed {
			return nil
		}
	}

	return nil
}

// removeRegistryVolume attempts to remove a volume by name.
// Returns true if the volume was successfully removed, false if it didn't exist.
func removeRegistryVolume(
	ctx context.Context,
	dockerClient client.APIClient,
	volumeName string,
) (bool, error) {
	trimmed := strings.TrimSpace(volumeName)
	if trimmed == "" {
		return false, nil
	}

	err := dockerClient.VolumeRemove(ctx, trimmed, false)
	if err != nil {
		if cerrdefs.IsNotFound(err) {
			return false, nil
		}

		return false, fmt.Errorf("failed to remove registry volume: %w", err)
	}

	return true, nil
}

// Utility helpers.

// uniqueNonEmpty returns unique non-empty strings from the input, preserving order.
func uniqueNonEmpty(values ...string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))

	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}

		if _, exists := seen[trimmed]; exists {
			continue
		}

		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}

	return result
}

// registryAttachedToOtherClusters checks if a registry is connected to cluster networks
// other than the one being ignored.
func registryAttachedToOtherClusters(
	inspect container.InspectResponse,
	ignoredNetwork string,
) bool {
	if inspect.NetworkSettings == nil || len(inspect.NetworkSettings.Networks) == 0 {
		return false
	}

	ignored := strings.ToLower(strings.TrimSpace(ignoredNetwork))

	for name := range inspect.NetworkSettings.Networks {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			continue
		}

		lower := strings.ToLower(trimmed)
		if ignored != "" && lower == ignored {
			continue
		}

		if isClusterNetworkName(lower) {
			return true
		}
	}

	return false
}

// isClusterNetworkName determines if a network name represents a cluster network.
func isClusterNetworkName(network string) bool {
	switch {
	case network == "":
		return false
	case network == "kind":
		return true
	case strings.HasPrefix(network, "kind-"):
		return true
	case network == "k3d":
		return true
	case strings.HasPrefix(network, "k3d-"):
		return true
	default:
		return false
	}
}
