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
	// CloudProviderKindImage is the official cloud-provider-kind image.
	CloudProviderKindImage = "registry.k8s.io/cloud-provider-kind/cloud-controller-manager:latest"
	// CloudProviderKindContainerName is the name of the cloud-provider-kind container.
	CloudProviderKindContainerName = "cloud-provider-kind"
	// CloudProviderKindNetworkName is the default KIND network name.
	CloudProviderKindNetworkName = "kind"
	// DockerSocketPath is the path to the Docker socket.
	DockerSocketPath = "/var/run/docker.sock"
)

// CloudProviderKINDInstaller manages the cloud-provider-kind Docker container.
type CloudProviderKINDInstaller struct {
	client client.APIClient
}

// NewCloudProviderKINDInstaller creates a new Cloud Provider KIND installer instance.
func NewCloudProviderKINDInstaller(
	dockerClient client.APIClient,
) *CloudProviderKINDInstaller {
	return &CloudProviderKINDInstaller{
		client: dockerClient,
	}
}

// Install starts the cloud-provider-kind container if not already running.
func (c *CloudProviderKINDInstaller) Install(ctx context.Context) error {
	// Check if container is already running
	running, err := c.isContainerRunning(ctx)
	if err != nil {
		return fmt.Errorf("failed to check if container is running: %w", err)
	}

	if running {
		return nil // Already running
	}

	// Ensure the cloud-provider-kind image is available
	err = c.ensureImage(ctx)
	if err != nil {
		return fmt.Errorf("failed to ensure cloud-provider-kind image: %w", err)
	}

	// Create and start the container
	err = c.createAndStartContainer(ctx)
	if err != nil {
		return fmt.Errorf("failed to create and start cloud-provider-kind container: %w", err)
	}

	return nil
}

// Uninstall stops and removes the cloud-provider-kind container.
func (c *CloudProviderKINDInstaller) Uninstall(ctx context.Context) error {
	containers, err := c.listContainers(ctx)
	if err != nil {
		return fmt.Errorf("failed to list containers: %w", err)
	}

	if len(containers) == 0 {
		return nil // Nothing to uninstall
	}

	// Stop and remove container
	for _, ctr := range containers {
		// Stop container if running
		if strings.EqualFold(ctr.State, "running") {
			err = c.client.ContainerStop(ctx, ctr.ID, container.StopOptions{})
			if err != nil {
				return fmt.Errorf("failed to stop container: %w", err)
			}
		}

		// Remove container
		err = c.client.ContainerRemove(ctx, ctr.ID, container.RemoveOptions{
			Force: true,
		})
		if err != nil {
			return fmt.Errorf("failed to remove container: %w", err)
		}
	}

	return nil
}

// --- internals ---

// isContainerRunning checks if the cloud-provider-kind container is running.
func (c *CloudProviderKINDInstaller) isContainerRunning(ctx context.Context) (bool, error) {
	containers, err := c.listContainers(ctx)
	if err != nil {
		return false, err
	}

	if len(containers) == 0 {
		return false, nil
	}

	return strings.EqualFold(containers[0].State, "running"), nil
}

// listContainers lists all cloud-provider-kind containers.
func (c *CloudProviderKINDInstaller) listContainers(ctx context.Context) ([]container.Summary, error) {
	filterArgs := filters.NewArgs()
	filterArgs.Add("name", "^/"+CloudProviderKindContainerName+"$")

	containers, err := c.client.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filterArgs,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	return containers, nil
}

// ensureImage pulls the cloud-provider-kind image if not already present.
func (c *CloudProviderKINDInstaller) ensureImage(ctx context.Context) error {
	// Check if image exists
	_, err := c.client.ImageInspect(ctx, CloudProviderKindImage)
	if err == nil {
		return nil // Image already exists
	}

	// Pull image
	reader, err := c.client.ImagePull(ctx, CloudProviderKindImage, image.PullOptions{})
	if err != nil {
		return fmt.Errorf("failed to pull image: %w", err)
	}
	defer reader.Close()

	// Consume pull output
	_, err = io.Copy(io.Discard, reader)
	if err != nil {
		return fmt.Errorf("failed to read image pull output: %w", err)
	}

	return nil
}

// createAndStartContainer creates and starts the cloud-provider-kind container.
func (c *CloudProviderKINDInstaller) createAndStartContainer(ctx context.Context) error {
	// Container configuration
	containerConfig := &container.Config{
		Image: CloudProviderKindImage,
	}

	// Host configuration - mount Docker socket
	hostConfig := &container.HostConfig{
		Mounts: []mount.Mount{
			{
				Type:   mount.TypeBind,
				Source: DockerSocketPath,
				Target: DockerSocketPath,
			},
		},
		RestartPolicy: container.RestartPolicy{
			Name: "unless-stopped",
		},
	}

	// Network configuration - connect to KIND network
	networkConfig := &network.NetworkingConfig{
		EndpointsConfig: map[string]*network.EndpointSettings{
			CloudProviderKindNetworkName: {},
		},
	}

	// Create container
	resp, err := c.client.ContainerCreate(
		ctx,
		containerConfig,
		hostConfig,
		networkConfig,
		nil,
		CloudProviderKindContainerName,
	)
	if err != nil {
		return fmt.Errorf("failed to create container: %w", err)
	}

	// Start container
	err = c.client.ContainerStart(ctx, resp.ID, container.StartOptions{})
	if err != nil {
		return fmt.Errorf("failed to start container: %w", err)
	}

	return nil
}
