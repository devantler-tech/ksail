package docker

import (
	"context"
	"fmt"
	"strings"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/client"
)

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
