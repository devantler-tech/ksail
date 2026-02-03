package cloudproviderkindinstaller

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
)

const (
	// ContainerName is the name of the cloud-provider-kind container managed by KSail.
	ContainerName = "ksail-cloud-provider-kind"

	// KindNetworkName is the Docker network used by Kind clusters.
	KindNetworkName = "kind"

	// dockerSocketPath is the path to the Docker socket inside the container.
	dockerSocketPath = "/var/run/docker.sock"

	// hostDockerSocketPath is the path to the Docker socket on the host.
	hostDockerSocketPath = "/var/run/docker.sock"

	// cpkContainerPrefix is the prefix used by cloud-provider-kind for service LoadBalancer containers.
	cpkContainerPrefix = "cpk-"
)

// CloudProviderKINDInstaller manages the cloud-provider-kind controller as a Docker container.
type CloudProviderKINDInstaller struct {
	dockerClient client.APIClient
}

// NewCloudProviderKINDInstaller creates a new Cloud Provider KIND installer instance.
func NewCloudProviderKINDInstaller(dockerClient client.APIClient) *CloudProviderKINDInstaller {
	return &CloudProviderKINDInstaller{
		dockerClient: dockerClient,
	}
}

// Install starts the cloud-provider-kind controller container if not already running.
// The controller runs as a Docker container and monitors all KIND clusters for
// LoadBalancer services, creating additional containers to handle traffic.
func (c *CloudProviderKINDInstaller) Install(ctx context.Context) error {
	// Check if container already exists and is running
	running, err := c.isContainerRunning(ctx)
	if err != nil {
		return fmt.Errorf("check container status: %w", err)
	}

	if running {
		return nil // Already running, nothing to do
	}

	// Check if container exists but is stopped
	exists, err := c.containerExists(ctx)
	if err != nil {
		return fmt.Errorf("check container exists: %w", err)
	}

	if exists {
		// Container exists but not running, start it
		err = c.dockerClient.ContainerStart(ctx, ContainerName, container.StartOptions{})
		if err != nil {
			return fmt.Errorf("start existing container: %w", err)
		}

		return nil
	}

	// Container doesn't exist, create and start it
	err = c.createAndStartContainer(ctx)
	if err != nil {
		return fmt.Errorf("create container: %w", err)
	}

	return nil
}

// Uninstall stops and removes the cloud-provider-kind controller container.
// It also cleans up any cpk-* containers created by cloud-provider-kind for LoadBalancer services.
func (c *CloudProviderKINDInstaller) Uninstall(ctx context.Context) error {
	// Stop and remove the main controller container
	err := c.removeContainer(ctx, ContainerName)
	if err != nil {
		return fmt.Errorf("remove controller container: %w", err)
	}

	// Clean up cpk-* containers created by cloud-provider-kind
	err = c.cleanupCPKContainers(ctx)
	if err != nil {
		return fmt.Errorf("cleanup cpk containers: %w", err)
	}

	return nil
}

// isContainerRunning checks if the cloud-provider-kind container is running.
func (c *CloudProviderKINDInstaller) isContainerRunning(ctx context.Context) (bool, error) {
	containers, err := c.listContainersByName(ctx, ContainerName)
	if err != nil {
		return false, err
	}

	if len(containers) == 0 {
		return false, nil
	}

	return strings.EqualFold(containers[0].State, "running"), nil
}

// containerExists checks if the cloud-provider-kind container exists (running or stopped).
func (c *CloudProviderKINDInstaller) containerExists(ctx context.Context) (bool, error) {
	containers, err := c.listContainersByName(ctx, ContainerName)
	if err != nil {
		return false, err
	}

	return len(containers) > 0, nil
}

// listContainersByName lists containers matching the given exact name.
func (c *CloudProviderKINDInstaller) listContainersByName(
	ctx context.Context,
	name string,
) ([]container.Summary, error) {
	filterArgs := filters.NewArgs()
	filterArgs.Add("name", "^/"+name+"$")

	containers, err := c.dockerClient.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filterArgs,
	})
	if err != nil {
		return nil, fmt.Errorf("list containers: %w", err)
	}

	return containers, nil
}

// createAndStartContainer creates and starts the cloud-provider-kind container.
func (c *CloudProviderKINDInstaller) createAndStartContainer(ctx context.Context) error {
	imageName := CloudProviderKindImage()

	// Ensure image exists
	err := c.ensureImage(ctx, imageName)
	if err != nil {
		return fmt.Errorf("ensure image: %w", err)
	}

	// Ensure the kind network exists
	err = c.ensureKindNetwork(ctx)
	if err != nil {
		return fmt.Errorf("ensure kind network: %w", err)
	}

	// Build container configuration
	containerConfig := &container.Config{
		Image: imageName,
		Labels: map[string]string{
			"app.kubernetes.io/name":       "cloud-provider-kind",
			"app.kubernetes.io/managed-by": "ksail",
		},
	}

	hostConfig := &container.HostConfig{
		RestartPolicy: container.RestartPolicy{
			Name: container.RestartPolicyUnlessStopped,
		},
		Mounts: []mount.Mount{
			{
				Type:   mount.TypeBind,
				Source: hostDockerSocketPath,
				Target: dockerSocketPath,
			},
		},
	}

	networkConfig := &network.NetworkingConfig{
		EndpointsConfig: map[string]*network.EndpointSettings{
			KindNetworkName: {},
		},
	}

	// Create container
	resp, err := c.dockerClient.ContainerCreate(
		ctx,
		containerConfig,
		hostConfig,
		networkConfig,
		nil,
		ContainerName,
	)
	if err != nil {
		return fmt.Errorf("create container: %w", err)
	}

	// Start container
	err = c.dockerClient.ContainerStart(ctx, resp.ID, container.StartOptions{})
	if err != nil {
		return fmt.Errorf("start container: %w", err)
	}

	return nil
}

// ensureImage pulls the image if not already present locally.
func (c *CloudProviderKINDInstaller) ensureImage(ctx context.Context, imageName string) error {
	// Check if image exists
	_, err := c.dockerClient.ImageInspect(ctx, imageName)
	if err == nil {
		return nil // Image exists
	}

	// Pull image
	reader, err := c.dockerClient.ImagePull(ctx, imageName, image.PullOptions{})
	if err != nil {
		return fmt.Errorf("pull image: %w", err)
	}

	defer func() { _ = reader.Close() }()

	// Consume pull output to complete the operation
	_, err = io.Copy(io.Discard, reader)
	if err != nil {
		return fmt.Errorf("read pull output: %w", err)
	}

	return nil
}

// ensureKindNetwork creates the kind network if it doesn't exist.
func (c *CloudProviderKINDInstaller) ensureKindNetwork(ctx context.Context) error {
	// Check if network exists
	_, err := c.dockerClient.NetworkInspect(ctx, KindNetworkName, network.InspectOptions{})
	if err == nil {
		return nil // Network exists
	}

	// Create network
	_, err = c.dockerClient.NetworkCreate(ctx, KindNetworkName, network.CreateOptions{})
	if err != nil {
		// Ignore "already exists" errors (race condition)
		if !strings.Contains(err.Error(), "already exists") {
			return fmt.Errorf("create network: %w", err)
		}
	}

	return nil
}

// removeContainer stops and removes a container by name.
func (c *CloudProviderKINDInstaller) removeContainer(ctx context.Context, name string) error {
	containers, err := c.listContainersByName(ctx, name)
	if err != nil {
		return err
	}

	if len(containers) == 0 {
		return nil // Container doesn't exist
	}

	containerID := containers[0].ID

	// Stop container if running
	if strings.EqualFold(containers[0].State, "running") {
		err = c.dockerClient.ContainerStop(ctx, containerID, container.StopOptions{})
		if err != nil {
			return fmt.Errorf("stop container: %w", err)
		}
	}

	// Remove container
	err = c.dockerClient.ContainerRemove(ctx, containerID, container.RemoveOptions{})
	if err != nil {
		return fmt.Errorf("remove container: %w", err)
	}

	return nil
}

// cleanupCPKContainers removes all cpk-* containers created by cloud-provider-kind.
func (c *CloudProviderKINDInstaller) cleanupCPKContainers(ctx context.Context) error {
	// List all containers with cpk- prefix
	filterArgs := filters.NewArgs()
	filterArgs.Add("name", cpkContainerPrefix)

	containers, err := c.dockerClient.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filterArgs,
	})
	if err != nil {
		return fmt.Errorf("list cpk containers: %w", err)
	}

	for _, cont := range containers {
		// Stop container if running
		if strings.EqualFold(cont.State, "running") {
			_ = c.dockerClient.ContainerStop(ctx, cont.ID, container.StopOptions{})
		}

		// Remove container
		_ = c.dockerClient.ContainerRemove(ctx, cont.ID, container.RemoveOptions{})
	}

	return nil
}
