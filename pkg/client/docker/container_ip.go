package docker

import (
	"context"
	"errors"
	"fmt"

	"github.com/docker/docker/client"
)

// Errors for container IP resolution.
var (
	// ErrNoNetworkSettings is returned when a container has no network configuration.
	ErrNoNetworkSettings = errors.New("container has no network settings")
	// ErrNotConnectedToNetwork is returned when a container is not attached to the specified network.
	ErrNotConnectedToNetwork = errors.New("container is not connected to network")
	// ErrNoIPAddress is returned when a container has no IP address on the specified network.
	ErrNoIPAddress = errors.New("container has no IP address on network")
)

// ResolveContainerIPOnNetwork inspects a Docker container and returns its IP address
// on the specified network. This is used when pods inside a virtual cluster need to
// reach a registry container by IP (since Docker DNS names are not resolvable from
// within Kubernetes CoreDNS).
func ResolveContainerIPOnNetwork(
	ctx context.Context,
	dockerClient client.APIClient,
	containerName string,
	networkName string,
) (string, error) {
	inspect, err := dockerClient.ContainerInspect(ctx, containerName)
	if err != nil {
		return "", fmt.Errorf("inspect container %s: %w", containerName, err)
	}

	if inspect.NetworkSettings == nil || inspect.NetworkSettings.Networks == nil {
		return "", fmt.Errorf("%w: %s", ErrNoNetworkSettings, containerName)
	}

	network, ok := inspect.NetworkSettings.Networks[networkName]
	if !ok {
		return "", fmt.Errorf(
			"%w: container %s, network %s", ErrNotConnectedToNetwork, containerName, networkName,
		)
	}

	ipAddress := network.IPAddress
	if ipAddress == "" {
		return "", fmt.Errorf(
			"%w: container %s, network %s", ErrNoIPAddress, containerName, networkName,
		)
	}

	return ipAddress, nil
}
