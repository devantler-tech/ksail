package registry

import (
	"context"
	"errors"
	"fmt"
	"io"
)

// Manager orchestrates registry lifecycle operations shared between mirror and local registries.
type Manager struct {
	backend Backend
}

// errRegistryBackendRequired ensures manager construction always validates input.
var errRegistryBackendRequired = errors.New("registry backend is required")

// NewManager creates a manager backed by the provided registry backend.
func NewManager(backend Backend) (*Manager, error) {
	if backend == nil {
		return nil, errRegistryBackendRequired
	}

	return &Manager{backend: backend}, nil
}

// EnsureBatch creates all requested registries as an atomic batch. Any failure rolls back prior creations.
func (c *Manager) EnsureBatch(
	ctx context.Context,
	registries []Info,
	clusterName string,
	networkName string,
	writer io.Writer,
) error {
	return SetupRegistries(
		ctx,
		c.backend,
		registries,
		clusterName,
		networkName,
		writer,
	)
}

// EnsureOne provisions a single registry and reports whether a new container was created.
func (c *Manager) EnsureOne(
	ctx context.Context,
	spec Info,
	clusterName string,
	writer io.Writer,
) (bool, error) {
	tracker, err := newMirrorBatch(ctx, c.backend, clusterName, "", writer, 1)
	if err != nil {
		return false, fmt.Errorf("create registry tracker: %w", err)
	}

	created, ensureErr := tracker.ensure(ctx, spec)
	if ensureErr != nil {
		tracker.rollback(ctx)

		return false, fmt.Errorf("ensure registry %s: %w", spec.Name, ensureErr)
	}

	return created, nil
}

// Cleanup removes the provided registries via the backend.
func (c *Manager) Cleanup(
	ctx context.Context,
	registries []Info,
	clusterName string,
	deleteVolumes bool,
	networkName string,
	writer io.Writer,
) error {
	return CleanupRegistries(
		ctx,
		c.backend,
		registries,
		clusterName,
		deleteVolumes,
		networkName,
		writer,
	)
}

// CleanupOne removes a single registry spec.
func (c *Manager) CleanupOne(
	ctx context.Context,
	registry Info,
	clusterName string,
	deleteVolume bool,
	networkName string,
) error {
	err := c.backend.DeleteRegistry(
		ctx,
		registry.Name,
		clusterName,
		deleteVolume,
		networkName,
		registry.Volume,
	)
	if err != nil {
		return fmt.Errorf("delete registry %s: %w", registry.Name, err)
	}

	return nil
}
