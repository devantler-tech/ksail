package kindprovisioner

import (
	"fmt"

	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider"
	dockerprovider "github.com/devantler-tech/ksail/v7/pkg/svc/provider/docker"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

// resolveKubeconfigPath resolves the effective kubeconfig path for kind
// through the shared resolver (explicit path → first KUBECONFIG entry →
// ~/.kube/config), matching kind's own --kubeconfig fallback semantics so
// env-based setups keep working when no path is configured.
func resolveKubeconfigPath(kubeconfigPath string) (string, error) {
	resolved, err := k8s.ResolveKubeconfigPath(kubeconfigPath)
	if err != nil {
		return "", fmt.Errorf("resolve kubeconfig path: %w", err)
	}

	return resolved, nil
}

// CreateProvisioner creates a Provisioner from a pre-loaded configuration.
// The Kind config should be loaded via the configmanager before calling this function,
// allowing any in-memory modifications (e.g., mirror registries) to be preserved.
//
// Parameters:
//   - kindConfig: Pre-loaded Kind cluster configuration
//   - kubeconfigPath: Path where the kubeconfig should be written (defaults to
//     the first KUBECONFIG entry, then ~/.kube/config)
func CreateProvisioner(
	kindConfig *v1alpha4.Cluster,
	kubeconfigPath string,
) (*Provisioner, error) {
	kubeconfigPath, err := resolveKubeconfigPath(kubeconfigPath)
	if err != nil {
		return nil, err
	}

	kindSDKProvider := NewDefaultProviderAdapter()

	dockerClient, err := NewDefaultDockerClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create Docker client: %w", err)
	}

	// Create Docker infrastructure provider for Kind clusters
	infraProvider := dockerprovider.NewProvider(dockerClient, dockerprovider.LabelSchemeKind)

	provisioner := NewProvisioner(
		kindConfig,
		kubeconfigPath,
		kindSDKProvider,
		infraProvider,
	)

	return provisioner, nil
}

// CreateProvisionerWithProvider creates a Provisioner with a custom infrastructure provider.
// This is useful for testing or when a specific provider implementation is needed.
func CreateProvisionerWithProvider(
	kindConfig *v1alpha4.Cluster,
	kubeconfigPath string,
	infraProvider provider.Provider,
) (*Provisioner, error) {
	kubeconfigPath, err := resolveKubeconfigPath(kubeconfigPath)
	if err != nil {
		return nil, err
	}

	kindSDKProvider := NewDefaultProviderAdapter()

	provisioner := NewProvisioner(
		kindConfig,
		kubeconfigPath,
		kindSDKProvider,
		infraProvider,
	)

	return provisioner, nil
}
