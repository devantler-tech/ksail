package kindprovisioner

import (
	"fmt"

	"github.com/devantler-tech/ksail/v5/pkg/client/docker"
	"github.com/docker/docker/client"
	"sigs.k8s.io/kind/pkg/cluster"
)

// DefaultProviderAdapter provides a production-ready implementation of Provider
// that wraps the kind library's Provider.
type DefaultProviderAdapter struct {
	provider *cluster.Provider
}

// NewDefaultProviderAdapter creates a new instance of the default Kind provider adapter.
// It initializes the underlying kind Provider with default options.
func NewDefaultProviderAdapter() *DefaultProviderAdapter {
	return &DefaultProviderAdapter{
		provider: cluster.NewProvider(),
	}
}

// Create creates a new kind cluster.
func (a *DefaultProviderAdapter) Create(name string, opts ...cluster.CreateOption) error {
	err := a.provider.Create(name, opts...)
	if err != nil {
		return fmt.Errorf("kind create: %w", err)
	}

	return nil
}

// Delete deletes a kind cluster.
func (a *DefaultProviderAdapter) Delete(name, kubeconfigPath string) error {
	err := a.provider.Delete(name, kubeconfigPath)
	if err != nil {
		return fmt.Errorf("kind delete: %w", err)
	}

	return nil
}

// List lists all kind clusters.
func (a *DefaultProviderAdapter) List() ([]string, error) {
	clusters, err := a.provider.List()
	if err != nil {
		return nil, fmt.Errorf("kind list: %w", err)
	}

	return clusters, nil
}

// ListNodes lists all nodes in a kind cluster.
func (a *DefaultProviderAdapter) ListNodes(name string) ([]string, error) {
	nodes, err := a.provider.ListNodes(name)
	if err != nil {
		return nil, fmt.Errorf("kind list nodes: %w", err)
	}

	// Convert nodes.Node slice to string slice (node names)
	nodeNames := make([]string, len(nodes))
	for i, node := range nodes {
		nodeNames[i] = node.String()
	}

	return nodeNames, nil
}

// NewDefaultDockerClient creates a new Docker client using environment configuration.
// This provides a production-ready implementation for the ContainerAPIClient interface
// required by Provisioner.
// Returns the concrete type to satisfy ireturn linter.
func NewDefaultDockerClient() (*client.Client, error) {
	c, err := docker.GetConcreteDockerClient()
	if err != nil {
		return nil, fmt.Errorf("create Docker client: %w", err)
	}

	return c, nil
}
