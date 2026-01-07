package docker

import (
	"context"
	"fmt"
	"strings"

	"github.com/docker/docker/api/types/container"
)

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

	// Prepare registry resources (volume)
	volumeName, err := rm.prepareRegistryResources(ctx, config)
	if err != nil {
		return fmt.Errorf("failed to prepare registry resources: %w", err)
	}

	// Create and start the container
	return rm.createAndStartContainer(ctx, config, volumeName)
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

// DisconnectFromNetwork disconnects a registry container from a specific network.
// This allows the network to be removed without affecting the registry container.
// The registry will continue running and can be reconnected to a network later.
func (rm *RegistryManager) DisconnectFromNetwork(
	ctx context.Context,
	name string,
	networkName string,
) error {
	trimmedNetwork := strings.TrimSpace(networkName)
	if trimmedNetwork == "" {
		return nil // No network specified, nothing to disconnect
	}

	containers, err := rm.listRegistryContainers(ctx, name)
	if err != nil {
		return fmt.Errorf("failed to list registry containers: %w", err)
	}

	if len(containers) == 0 {
		return nil // Registry doesn't exist, nothing to disconnect
	}

	registryContainer := containers[0]

	inspect, err := inspectContainer(ctx, rm.client, registryContainer.ID)
	if err != nil {
		return err
	}

	_, err = disconnectRegistryNetwork(
		ctx,
		rm.client,
		registryContainer.ID,
		name,
		trimmedNetwork,
		inspect,
	)

	return err
}

// DisconnectAllFromNetwork disconnects all KSail registry containers from a specific network.
// This is useful for cleaning up network connections before deleting a cluster network.
// Returns the number of containers that were disconnected.
func (rm *RegistryManager) DisconnectAllFromNetwork(
	ctx context.Context,
	networkName string,
) (int, error) {
	trimmedNetwork := strings.TrimSpace(networkName)
	if trimmedNetwork == "" {
		return 0, nil
	}

	// List all KSail registry containers
	containers, err := rm.listAllRegistryContainers(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to list registry containers: %w", err)
	}

	if len(containers) == 0 {
		return 0, nil
	}

	disconnectedCount := 0

	for _, registryContainer := range containers {
		inspect, inspectErr := inspectContainer(ctx, rm.client, registryContainer.ID)
		if inspectErr != nil {
			// Container may have been removed, skip it
			continue
		}

		// Check if container is connected to the target network
		if _, connected := inspect.NetworkSettings.Networks[trimmedNetwork]; !connected {
			continue
		}

		// Get container name for the disconnect operation
		containerName := ""
		if label, ok := registryContainer.Labels[RegistryLabelKey]; ok {
			containerName = label
		} else if len(registryContainer.Names) > 0 {
			containerName = strings.TrimPrefix(registryContainer.Names[0], "/")
		}

		_, disconnectErr := disconnectRegistryNetwork(
			ctx,
			rm.client,
			registryContainer.ID,
			containerName,
			trimmedNetwork,
			inspect,
		)
		if disconnectErr != nil {
			return disconnectedCount, fmt.Errorf(
				"failed to disconnect container %s from network: %w",
				containerName,
				disconnectErr,
			)
		}

		disconnectedCount++
	}

	return disconnectedCount, nil
}
