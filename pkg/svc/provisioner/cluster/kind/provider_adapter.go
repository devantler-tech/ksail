package kindprovisioner

import (
	"errors"
	"fmt"

	"github.com/devantler-tech/ksail-go/pkg/client/docker"
	"github.com/docker/docker/client"
	"sigs.k8s.io/kind/pkg/cluster"
)

var errUnexpectedDockerClientType = errors.New("unexpected docker client type")

// DefaultKindProviderAdapter provides a production-ready implementation of KindProvider
// that wraps the kind library's Provider.
type DefaultKindProviderAdapter struct {
	provider *cluster.Provider
}

// NewDefaultKindProviderAdapter creates a new instance of the default Kind provider adapter.
// It initializes the underlying kind Provider with default options.
func NewDefaultKindProviderAdapter() *DefaultKindProviderAdapter {
	return &DefaultKindProviderAdapter{
		provider: cluster.NewProvider(),
	}
}

// Create creates a new kind cluster.
func (a *DefaultKindProviderAdapter) Create(name string, opts ...cluster.CreateOption) error {
	err := a.provider.Create(name, opts...)
	if err != nil {
		return fmt.Errorf("kind create: %w", err)
	}

	return nil
}

// Delete deletes a kind cluster.
func (a *DefaultKindProviderAdapter) Delete(name, kubeconfigPath string) error {
	err := a.provider.Delete(name, kubeconfigPath)
	if err != nil {
		return fmt.Errorf("kind delete: %w", err)
	}

	return nil
}

// List lists all kind clusters.
func (a *DefaultKindProviderAdapter) List() ([]string, error) {
	clusters, err := a.provider.List()
	if err != nil {
		return nil, fmt.Errorf("kind list: %w", err)
	}

	return clusters, nil
}

// ListNodes lists all nodes in a kind cluster.
func (a *DefaultKindProviderAdapter) ListNodes(name string) ([]string, error) {
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
// required by KindClusterProvisioner.
// Returns the concrete type to satisfy ireturn linter.
func NewDefaultDockerClient() (*client.Client, error) {
	dockerClient, err := docker.GetDockerClient()
	if err != nil {
		return nil, fmt.Errorf("create Docker client: %w", err)
	}

	clientPtr, ok := dockerClient.(*client.Client)
	if !ok {
		return nil, fmt.Errorf("%w: %T", errUnexpectedDockerClientType, dockerClient)
	}

	return clientPtr, nil
}
