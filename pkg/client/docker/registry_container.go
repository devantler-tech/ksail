package docker

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/devantler-tech/ksail/v5/pkg/utils/envvar"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/go-connections/nat"
)

// Query operations for registry containers.

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
// Returns ErrRegistryPortNotFound if the registry has no host port binding (e.g., mirror registries).
func (rm *RegistryManager) GetRegistryPort(ctx context.Context, name string) (int, error) {
	containers, err := rm.listRegistryContainers(ctx, name)
	if err != nil {
		return 0, fmt.Errorf("failed to list registry containers: %w", err)
	}

	if len(containers) == 0 {
		return 0, ErrRegistryNotFound
	}

	// Get port from container ports - look for a valid host port binding
	for _, port := range containers[0].Ports {
		if port.PrivatePort == DefaultRegistryPort && port.PublicPort > 0 {
			return int(port.PublicPort), nil
		}
	}

	return 0, ErrRegistryPortNotFound
}

// IsContainerRunning checks if a container with the given exact name is running.
// Unlike IsRegistryInUse, this method does not require KSail labels, making it
// suitable for detecting K3d-managed registries.
func (rm *RegistryManager) IsContainerRunning(ctx context.Context, name string) (bool, error) {
	containers, err := rm.listContainersByNameOnly(ctx, name)
	if err != nil {
		return false, fmt.Errorf("failed to list containers: %w", err)
	}

	if len(containers) == 0 {
		return false, nil
	}

	return containers[0].State == "running", nil
}

// GetContainerPort returns the host port for a container with the given exact name.
// Unlike GetRegistryPort, this method does not require KSail labels, making it
// suitable for detecting K3d-managed registries.
func (rm *RegistryManager) GetContainerPort(
	ctx context.Context,
	name string,
	privatePort uint16,
) (int, error) {
	containers, err := rm.listContainersByNameOnly(ctx, name)
	if err != nil {
		return 0, fmt.Errorf("failed to list containers: %w", err)
	}

	if len(containers) == 0 {
		return 0, ErrRegistryNotFound
	}

	for _, port := range containers[0].Ports {
		if port.PrivatePort == privatePort {
			return int(port.PublicPort), nil
		}
	}

	return 0, ErrRegistryPortNotFound
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

// listContainersByNameOnly lists containers matching the given name without requiring KSail labels.
// This is used for detecting K3d-managed registries which don't have KSail labels.
func (rm *RegistryManager) listContainersByNameOnly(
	ctx context.Context,
	name string,
) ([]container.Summary, error) {
	filterArgs := filters.NewArgs()
	// Use exact name match with regex anchor to avoid partial matches
	filterArgs.Add("name", "^/"+name+"$")

	containers, err := rm.client.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filterArgs,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list registry containers: %w", err)
	}

	return containers, nil
}

// FindContainerBySuffix finds a running container whose name ends with the given suffix.
// Returns the container name if found, or empty string if not found.
// This is used to detect cluster-prefixed registries (e.g., "*-local-registry").
func (rm *RegistryManager) FindContainerBySuffix(
	ctx context.Context,
	suffix string,
) (string, error) {
	// List all containers
	containers, err := rm.client.ContainerList(ctx, container.ListOptions{
		All: false, // Only running containers
	})
	if err != nil {
		return "", fmt.Errorf("failed to list containers: %w", err)
	}

	// Find first container matching the suffix
	for _, c := range containers {
		for _, name := range c.Names {
			// Container names are prefixed with "/"
			containerName := strings.TrimPrefix(name, "/")
			if strings.HasSuffix(containerName, suffix) {
				return containerName, nil
			}
		}
	}

	return "", nil
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

// addClusterLabel is a no-op with network-based tracking.
// Previously used for label-based tracking, now replaced by network connections.
// Kept for interface compatibility but may be removed in future refactoring.
func (rm *RegistryManager) addClusterLabel(
	_ context.Context,
	_, _ string,
) error {
	return nil
}

// Image management.

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

// Volume management.

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

// prepareRegistryResources creates the volume for a registry.
func (rm *RegistryManager) prepareRegistryResources(
	ctx context.Context,
	config RegistryConfig,
) (string, error) {
	// Create volume for registry data using a distribution-agnostic name for reuse
	volumeName := rm.resolveVolumeName(config)
	if volumeName == "" {
		volumeName = config.Name
	}

	err := rm.createVolume(ctx, volumeName)
	if err != nil {
		return "", fmt.Errorf("failed to create registry volume: %w", err)
	}

	return volumeName, nil
}

// Container creation.

// createAndStartContainer creates and starts a registry container.
func (rm *RegistryManager) createAndStartContainer(
	ctx context.Context,
	config RegistryConfig,
	volumeName string,
) error {
	// Prepare container configuration
	containerConfig := rm.buildContainerConfig(config)
	hostConfig := rm.buildHostConfig(config, volumeName)
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

// Configuration builders.

// buildContainerConfig builds the container configuration for a registry.
// If an upstream URL is provided, environment variables are set to configure
// the registry as a pull-through cache (proxy) to that upstream.
// If credentials are provided, they are expanded from environment variables
// and set as REGISTRY_PROXY_USERNAME and REGISTRY_PROXY_PASSWORD.
func (rm *RegistryManager) buildContainerConfig(
	config RegistryConfig,
) *container.Config {
	labels := map[string]string{}
	if config.Name != "" {
		labels[RegistryLabelKey] = config.Name
	}

	// Build environment variables for registry configuration
	var env []string
	if config.UpstreamURL != "" {
		// Configure registry as a pull-through cache to the upstream
		env = append(env, "REGISTRY_PROXY_REMOTEURL="+config.UpstreamURL)

		// Add authentication credentials if provided
		// Expand environment variable placeholders (e.g., ${GITHUB_TOKEN})
		if config.Username != "" || config.Password != "" {
			username := envvar.Expand(config.Username)
			password := envvar.Expand(config.Password)

			if username != "" {
				env = append(env, "REGISTRY_PROXY_USERNAME="+username)
			}

			if password != "" {
				env = append(env, "REGISTRY_PROXY_PASSWORD="+password)
			}
		}
	}

	return &container.Config{
		Image: RegistryImageName,
		ExposedPorts: nat.PortSet{
			RegistryContainerPort: struct{}{},
		},
		Labels: labels,
		Env:    env,
	}
}

// buildHostConfig builds the host configuration including port bindings and mounts.
// Mirror registries (those with UpstreamURL set) do not need host port bindings
// since they are accessed via Docker network by cluster nodes, not from the host.
func (rm *RegistryManager) buildHostConfig(
	config RegistryConfig,
	volumeName string,
) *container.HostConfig {
	portBindings := nat.PortMap{}
	// Only bind to host port for local registries (not mirrors)
	// Mirrors are accessed via Docker network, not from the host
	if config.Port > 0 && config.UpstreamURL == "" {
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

// GetUsedHostPorts returns a map of all host ports currently in use by running containers.
// This is useful for avoiding port conflicts when creating new registry containers.
func (rm *RegistryManager) GetUsedHostPorts(ctx context.Context) (map[int]struct{}, error) {
	containers, err := rm.client.ContainerList(ctx, container.ListOptions{
		All: false, // Only running containers
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	usedPorts := make(map[int]struct{})

	for _, c := range containers {
		for _, port := range c.Ports {
			if port.PublicPort > 0 {
				usedPorts[int(port.PublicPort)] = struct{}{}
			}
		}
	}

	return usedPorts, nil
}
