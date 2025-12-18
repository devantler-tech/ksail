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
