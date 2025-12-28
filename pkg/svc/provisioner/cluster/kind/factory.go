package kindprovisioner

import (
	"fmt"

	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

const defaultKubeconfigPath = "~/.kube/config"

// CreateProvisioner creates a KindClusterProvisioner from a pre-loaded configuration.
// The Kind config should be loaded via the config-manager before calling this function,
// allowing any in-memory modifications (e.g., mirror registries) to be preserved.
//
// Parameters:
//   - kindConfig: Pre-loaded Kind cluster configuration
//   - kubeconfigPath: Path where the kubeconfig should be written (defaults to ~/.kube/config)
func CreateProvisioner(
	kindConfig *v1alpha4.Cluster,
	kubeconfigPath string,
) (*KindClusterProvisioner, error) {
	if kubeconfigPath == "" {
		kubeconfigPath = defaultKubeconfigPath
	}

	provider := NewDefaultKindProviderAdapter()

	dockerClient, err := NewDefaultDockerClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create Docker client: %w", err)
	}

	provisioner := NewKindClusterProvisioner(
		kindConfig,
		kubeconfigPath,
		provider,
		dockerClient,
	)

	return provisioner, nil
}
