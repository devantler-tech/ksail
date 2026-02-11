package docker

import (
	"fmt"

	"github.com/docker/docker/client"
)

// Resources holds Docker client and registry manager for cleanup.
// Use NewResources to create an instance.
type Resources struct {
	Client          client.APIClient
	RegistryManager *RegistryManager
}

// NewResources creates a Docker client and registry manager.
// The caller is responsible for calling Close() on the returned Resources.
func NewResources() (*Resources, error) {
	dockerClient, err := client.NewClientWithOpts(
		client.FromEnv,
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		return nil, fmt.Errorf("create docker client: %w", err)
	}

	registryManager, err := NewRegistryManager(dockerClient)
	if err != nil {
		_ = dockerClient.Close()

		return nil, fmt.Errorf("create registry manager: %w", err)
	}

	return &Resources{
		Client:          dockerClient,
		RegistryManager: registryManager,
	}, nil
}

// Close releases the Docker client resources.
func (r *Resources) Close() {
	if r.Client != nil {
		_ = r.Client.Close()
	}
}
