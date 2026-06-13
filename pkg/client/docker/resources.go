package docker

import (
	"fmt"
)

// Resources holds Docker client and registry manager for cleanup.
// Use NewResources to create an instance.
type Resources struct {
	Client          Client
	RegistryManager *RegistryManager
}

// NewResources creates a Docker client and registry manager.
// The caller is responsible for calling Close() on the returned Resources.
func NewResources() (*Resources, error) {
	dockerClient, err := GetDockerClient()
	if err != nil {
		return nil, err
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
