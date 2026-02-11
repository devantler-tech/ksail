package registry

import (
	"context"
	"fmt"
	"sync"

	dockerclient "github.com/devantler-tech/ksail/v5/pkg/client/docker"
	"github.com/docker/docker/client"
)

// Backend defines the minimal registry operations required by both mirror and local registry flows.
type Backend interface {
	CreateRegistry(ctx context.Context, config dockerclient.RegistryConfig) error
	DeleteRegistry(
		ctx context.Context,
		name, clusterName string,
		deleteVolume bool,
		networkName string,
		volumeName string,
	) error
	ListRegistries(ctx context.Context) ([]string, error)
	GetRegistryPort(ctx context.Context, name string) (int, error)
	WaitForRegistriesReady(ctx context.Context, registryIPs map[string]string) error
}

// BackendFactory creates a Backend from a Docker API client.
// This abstraction allows tests to inject mock backends without creating real Docker containers.
type BackendFactory func(client.APIClient) (Backend, error)

// DefaultBackendFactory creates a real RegistryManager that interacts with Docker.
func DefaultBackendFactory(dockerClient client.APIClient) (Backend, error) {
	backend, err := dockerclient.NewRegistryManager(dockerClient)
	if err != nil {
		return nil, fmt.Errorf("creating registry manager: %w", err)
	}

	return backend, nil
}

// Package-level backend factory with test override support.
var (
	//nolint:gochecknoglobals // dependency injection for tests
	backendFactoryMu sync.RWMutex
	//nolint:gochecknoglobals // dependency injection for tests
	backendFactoryOverride BackendFactory
)

// GetBackendFactory returns the current backend factory, using the override if set.
func GetBackendFactory() BackendFactory {
	backendFactoryMu.RLock()
	defer backendFactoryMu.RUnlock()

	if backendFactoryOverride != nil {
		return backendFactoryOverride
	}

	return DefaultBackendFactory
}

// SetBackendFactoryForTests sets an override backend factory for testing.
// Returns a cleanup function that restores the previous factory.
func SetBackendFactoryForTests(factory BackendFactory) func() {
	backendFactoryMu.Lock()

	previous := backendFactoryOverride
	backendFactoryOverride = factory

	backendFactoryMu.Unlock()

	return func() {
		backendFactoryMu.Lock()

		backendFactoryOverride = previous

		backendFactoryMu.Unlock()
	}
}
