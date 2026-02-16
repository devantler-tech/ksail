package docker

import (
	"errors"
	"fmt"

	"github.com/docker/docker/client"
)

// Error definitions for container engine operations.
var (
	// ErrAPIClientNil is returned when apiClient is nil.
	ErrAPIClientNil = errors.New("apiClient cannot be nil")

	// ErrUnexpectedDockerClientType is returned when the Docker client has an unexpected concrete type.
	ErrUnexpectedDockerClientType = errors.New("unexpected docker client type")
)

// GetDockerClient creates a Docker client using environment configuration.
func GetDockerClient() (client.APIClient, error) {
	dockerClient, err := client.NewClientWithOpts(
		client.FromEnv,
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create Docker client: %w", err)
	}

	return dockerClient, nil
}

// GetConcreteDockerClient creates a Docker client and returns the concrete *client.Client type.
// This is useful for callers that need the concrete type rather than the APIClient interface.
func GetConcreteDockerClient() (*client.Client, error) {
	dockerClient, err := GetDockerClient()
	if err != nil {
		return nil, err
	}

	clientPtr, ok := dockerClient.(*client.Client)
	if !ok {
		return nil, fmt.Errorf("%w: %T", ErrUnexpectedDockerClientType, dockerClient)
	}

	return clientPtr, nil
}
