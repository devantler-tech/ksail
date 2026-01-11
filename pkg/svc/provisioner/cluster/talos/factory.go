package talosprovisioner

import (
	"fmt"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	talosconfigmanager "github.com/devantler-tech/ksail/v5/pkg/io/config-manager/talos"
	kindprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/kind"
)

// CreateProvisioner creates a TalosProvisioner from a pre-loaded configuration.
// The Talos config should be loaded via the config-manager before calling this function,
// allowing any in-memory modifications (e.g., CNI patches, mirror registries) to be preserved.
//
// Parameters:
//   - talosConfigs: Pre-loaded Talos machine configurations with all patches applied
//   - kubeconfigPath: Path where the kubeconfig should be written
//   - provider: Infrastructure provider backend (Docker)
//   - opts: Talos-specific options (node counts, etc.)
//   - skipCNIChecks: Whether to skip CNI-dependent checks (CoreDNS, kube-proxy) during bootstrap.
//     Set to true when KSail will install a custom CNI after cluster creation.
func CreateProvisioner(
	talosConfigs *talosconfigmanager.Configs,
	kubeconfigPath string,
	provider v1alpha1.Provider,
	opts v1alpha1.OptionsTalos,
	skipCNIChecks bool,
) (*TalosProvisioner, error) {
	// Validate provider for Talos distribution
	if err := provider.ValidateForDistribution(v1alpha1.DistributionTalos); err != nil {
		return nil, err
	}

	// Create options and apply configured node counts
	options := NewOptions().WithKubeconfigPath(kubeconfigPath).WithSkipCNIChecks(skipCNIChecks)
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
