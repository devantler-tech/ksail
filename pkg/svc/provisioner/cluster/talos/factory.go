package talosprovisioner

import (
	"errors"
	"fmt"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	talosconfigmanager "github.com/devantler-tech/ksail/v5/pkg/io/config-manager/talos"
	kindprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/kind"
)

// ErrUnsupportedTalosProvider is returned when an unsupported Talos provider is specified.
var ErrUnsupportedTalosProvider = errors.New("unsupported Talos provider")

// CreateProvisioner creates a TalosProvisioner from a pre-loaded configuration.
// The Talos config should be loaded via the config-manager before calling this function,
// allowing any in-memory modifications (e.g., CNI patches, mirror registries) to be preserved.
//
// Parameters:
//   - talosConfigs: Pre-loaded Talos machine configurations with all patches applied
//   - kubeconfigPath: Path where the kubeconfig should be written
//   - opts: Talos-specific options (node counts, provider, etc.)
func CreateProvisioner(
	talosConfigs *talosconfigmanager.Configs,
	kubeconfigPath string,
	opts v1alpha1.OptionsTalos,
) (*TalosProvisioner, error) {
	// Validate or default the provider
	provider := opts.Provider
	if provider == "" {
		provider = v1alpha1.TalosProviderDocker
	}

	// Currently only Docker provider is supported
	if provider != v1alpha1.TalosProviderDocker {
		return nil, fmt.Errorf("%w: %s (supported: %s)",
			ErrUnsupportedTalosProvider, provider, v1alpha1.TalosProviderDocker)
	}

	// Create options and apply configured node counts
	options := NewOptions().WithKubeconfigPath(kubeconfigPath)
	if opts.ControlPlanes > 0 {
		options.WithControlPlaneNodes(int(opts.ControlPlanes))
	}

	if opts.Workers > 0 {
		options.WithWorkerNodes(int(opts.Workers))
	}

	// Create provisioner with loaded configs and options
	provisioner := NewTalosProvisioner(talosConfigs, options)

	dockerClient, err := kindprovisioner.NewDefaultDockerClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create Docker client: %w", err)
	}

	provisioner.WithDockerClient(dockerClient)

	return provisioner, nil
}
