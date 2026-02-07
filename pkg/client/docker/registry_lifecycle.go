package docker

import (
	"context"
	"fmt"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
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
		return nil
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

// RegistryInfo contains basic information about a registry container.
type RegistryInfo struct {
	Name         string
	ID           string
	IsKSailOwned bool
}

// ListRegistriesOnNetwork returns all registry containers connected to a specific network.
// This includes both KSail-managed and non-KSail registries (like K3d-managed ones).
// It identifies registries by the "registry" image ancestor.
func (rm *RegistryManager) ListRegistriesOnNetwork(
	ctx context.Context,
	networkName string,
) ([]RegistryInfo, error) {
	trimmedNetwork := strings.TrimSpace(networkName)
	if trimmedNetwork == "" {
		return nil, nil
	}

	// List all containers with registry image (includes non-KSail registries)
	containers, err := rm.listAllRegistryImageContainers(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list registry containers: %w", err)
	}

	registries := make([]RegistryInfo, 0, len(containers))

	for _, containerInfo := range containers {
		regInfo, ok := rm.extractRegistryInfoIfOnNetwork(ctx, containerInfo, trimmedNetwork)
		if ok {
			registries = append(registries, regInfo)
		}
	}

	return registries, nil
}

// extractRegistryInfoIfOnNetwork checks if a container is on the target network and returns its info.
func (rm *RegistryManager) extractRegistryInfoIfOnNetwork(
	ctx context.Context,
	containerSummary container.Summary,
	networkName string,
) (RegistryInfo, bool) {
	inspect, inspectErr := inspectContainer(ctx, rm.client, containerSummary.ID)
	if inspectErr != nil {
		return RegistryInfo{}, false
	}

	// Check if container is connected to the target network
	if _, connected := inspect.NetworkSettings.Networks[networkName]; !connected {
		return RegistryInfo{}, false
	}

	// Get container name
	containerName := ""
	if len(containerSummary.Names) > 0 {
		containerName = strings.TrimPrefix(containerSummary.Names[0], "/")
	}

	// Check if KSail-managed
	_, isKSailOwned := containerSummary.Labels[RegistryLabelKey]

	return RegistryInfo{
		Name:         containerName,
		ID:           containerSummary.ID,
		IsKSailOwned: isKSailOwned,
	}, true
}

// DeleteRegistriesOnNetwork deletes all registry containers connected to a specific network.
// This is used for cleaning up registries when deleting non-scaffolded clusters.
// If deleteVolumes is true, associated volumes will also be deleted.
func (rm *RegistryManager) DeleteRegistriesOnNetwork(
	ctx context.Context,
	networkName string,
	deleteVolumes bool,
) ([]string, error) {
	registries, err := rm.ListRegistriesOnNetwork(ctx, networkName)
	if err != nil {
		return nil, err
	}

	deletedNames := make([]string, 0, len(registries))

	for _, reg := range registries {
		// Disconnect from network first
		disconnectErr := rm.DisconnectFromNetwork(ctx, reg.Name, networkName)
		if disconnectErr != nil {
			// Log disconnect failure but continue - the registry may already be disconnected
			// or we may still be able to delete it. This prevents cleanup from failing entirely
			// when a single registry has network issues.
			continue
		}

		deleted := rm.deleteRegistryContainer(ctx, reg, deleteVolumes)
		if deleted {
			deletedNames = append(deletedNames, reg.Name)
		}
	}

	return deletedNames, nil
}

// deleteRegistryContainer stops, removes a registry container, and optionally its volume.
// Returns true if the container was successfully deleted.
func (rm *RegistryManager) deleteRegistryContainer(
	ctx context.Context,
	reg RegistryInfo,
	deleteVolumes bool,
) bool {
	volumeName := rm.getRegistryVolumeName(ctx, reg.ID, deleteVolumes)

	// Stop container
	stopErr := rm.client.ContainerStop(ctx, reg.ID, container.StopOptions{})
	if stopErr != nil {
		return false
	}

	// Remove container
	removeErr := rm.client.ContainerRemove(ctx, reg.ID, container.RemoveOptions{})
	if removeErr != nil {
		return false
	}

	// Delete volume if requested
	if deleteVolumes && volumeName != "" {
		_ = rm.client.VolumeRemove(ctx, volumeName, false)
	}

	return true
}

// getRegistryVolumeName returns the volume name for a registry container if deleteVolumes is true.
func (rm *RegistryManager) getRegistryVolumeName(
	ctx context.Context,
	containerID string,
	deleteVolumes bool,
) string {
	if !deleteVolumes {
		return ""
	}

	inspect, inspectErr := inspectContainer(ctx, rm.client, containerID)
	if inspectErr != nil {
		return ""
	}

	for _, mount := range inspect.Mounts {
		if mount.Destination == RegistryDataPath {
			return mount.Name
		}
	}

	return ""
}

// listAllRegistryImageContainers lists all containers using the registry image (any registry).
func (rm *RegistryManager) listAllRegistryImageContainers(
	ctx context.Context,
) ([]container.Summary, error) {
	filterArgs := filters.NewArgs()
	filterArgs.Add("ancestor", RegistryImageName)

	containers, err := rm.client.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filterArgs,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list registry containers: %w", err)
	}

	return containers, nil
}

// DeleteRegistriesByInfo deletes registry containers using pre-discovered registry information.
// This is used when registries need to be discovered before cluster deletion (e.g., Talos)
// but deleted afterward, as the network may no longer exist.
func (rm *RegistryManager) DeleteRegistriesByInfo(
	ctx context.Context,
	registries []RegistryInfo,
	deleteVolumes bool,
) ([]string, error) {
	deletedNames := make([]string, 0, len(registries))

	for _, reg := range registries {
		if deleted := rm.deleteRegistryContainer(ctx, reg, deleteVolumes); deleted {
			deletedNames = append(deletedNames, reg.Name)
		}
	}

	return deletedNames, nil
}
