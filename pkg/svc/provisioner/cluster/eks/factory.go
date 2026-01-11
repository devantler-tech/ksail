package eksprovisioner

import (
	"fmt"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/docker/docker/client"
	"sigs.k8s.io/kind/pkg/cluster"
)

const defaultKubeconfigPath = "~/.kube/config"

// CreateProvisioner creates an EKSClusterProvisioner from a pre-loaded configuration.
//
// Parameters:
//   - eksConfig: Pre-loaded EKS Anywhere cluster configuration
//   - kubeconfigPath: Path where the kubeconfig should be written (defaults to ~/.kube/config)
//   - provider: Infrastructure provider backend (Docker)
func CreateProvisioner(
	eksConfig *EKSConfig,
	kubeconfigPath string,
	provider v1alpha1.Provider,
) (*EKSClusterProvisioner, error) {
	if kubeconfigPath == "" {
		kubeconfigPath = defaultKubeconfigPath
	}

	// Validate provider for EKS distribution
	if err := provider.ValidateForDistribution(v1alpha1.DistributionEKS); err != nil {
		return nil, err
	}

	kindProvider := NewDefaultKindProviderAdapter()

	dockerClient, err := NewDefaultDockerClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create Docker client: %w", err)
	}

	provisioner := NewEKSClusterProvisioner(
		eksConfig,
		kubeconfigPath,
		kindProvider,
		dockerClient,
	)

	return provisioner, nil
}

// KindProviderAdapter wraps sigs.k8s.io/kind/pkg/cluster.Provider to implement the KindProvider interface.
type KindProviderAdapter struct {
	p *cluster.Provider
}

// NewDefaultKindProviderAdapter creates a new KindProviderAdapter with default settings.
func NewDefaultKindProviderAdapter() *KindProviderAdapter {
	return &KindProviderAdapter{p: cluster.NewProvider()}
}

// Create creates a new Kind cluster.
func (k *KindProviderAdapter) Create(name string, opts ...cluster.CreateOption) error {
	return k.p.Create(name, opts...)
}

// Delete deletes a Kind cluster.
func (k *KindProviderAdapter) Delete(name, kubeconfigPath string) error {
	return k.p.Delete(name, kubeconfigPath)
}

// List returns all Kind clusters.
func (k *KindProviderAdapter) List() ([]string, error) {
	return k.p.List()
}

// ListNodes returns all nodes for a Kind cluster.
func (k *KindProviderAdapter) ListNodes(name string) ([]string, error) {
	nodes, err := k.p.ListNodes(name)
	if err != nil {
		return nil, err
	}

	nodeNames := make([]string, 0, len(nodes))
	for _, n := range nodes {
		nodeNames = append(nodeNames, n.String())
	}

	return nodeNames, nil
}

// NewDefaultDockerClient creates a new Docker client with default options.
func NewDefaultDockerClient() (client.ContainerAPIClient, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, err
	}

	return cli, nil
}
