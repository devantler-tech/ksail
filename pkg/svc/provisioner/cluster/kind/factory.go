package kindprovisioner

import (
	"fmt"

	"github.com/devantler-tech/ksail/v5/pkg/svc/provider"
	dockerprovider "github.com/devantler-tech/ksail/v5/pkg/svc/provider/docker"
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

	kindSDKProvider := NewDefaultKindProviderAdapter()

	dockerClient, err := NewDefaultDockerClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create Docker client: %w", err)
	}

	// Create Docker infrastructure provider for Kind clusters
	infraProvider := dockerprovider.NewDockerProvider(dockerClient, dockerprovider.LabelSchemeKind)

	provisioner := NewKindClusterProvisioner(
		kindConfig,
		kubeconfigPath,
		kindSDKProvider,
		infraProvider,
	)

	return provisioner, nil
}

// CreateProvisionerWithProvider creates a KindClusterProvisioner with a custom infrastructure provider.
// This is useful for testing or when a specific provider implementation is needed.
func CreateProvisionerWithProvider(
	kindConfig *v1alpha4.Cluster,
	kubeconfigPath string,
	infraProvider provider.Provider,
) (*KindClusterProvisioner, error) {
	if kubeconfigPath == "" {
		kubeconfigPath = defaultKubeconfigPath
	}

	kindSDKProvider := NewDefaultKindProviderAdapter()

	provisioner := NewKindClusterProvisioner(
		kindConfig,
		kubeconfigPath,
		kindSDKProvider,
		infraProvider,
	)

	return provisioner, nil
}
