package talosprovisioner

import (
	"errors"
	"fmt"
	"os"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	talosconfigmanager "github.com/devantler-tech/ksail/v5/pkg/io/config-manager/talos"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provider"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provider/docker"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provider/hetzner"
	clustererrors "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/errors"
	kindprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/kind"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
)

// ErrUnsupportedProvider re-exports the shared error for backward compatibility.
var ErrUnsupportedProvider = clustererrors.ErrUnsupportedProvider

// ErrMissingHetznerToken is returned when the Hetzner API token is not set.
var ErrMissingHetznerToken = errors.New("hetzner API token not set")

// CreateProvisioner creates a TalosProvisioner from a pre-loaded configuration.
// The Talos config should be loaded via the config-manager before calling this function,
// allowing any in-memory modifications (e.g., CNI patches, mirror registries) to be preserved.
//
// Parameters:
//   - talosConfigs: Pre-loaded Talos machine configurations with all patches applied
//   - kubeconfigPath: Path where the kubeconfig should be written
//   - provider: Infrastructure provider (e.g., Docker, Hetzner)
//   - opts: Talos-specific options (node counts, etc.)
//   - hetznerOpts: Hetzner-specific options (required when provider is Hetzner)
//   - skipCNIChecks: Whether to skip CNI-dependent checks (CoreDNS, kube-proxy) during bootstrap.
//     Set to true when KSail will install a custom CNI after cluster creation.
func CreateProvisioner(
	talosConfigs *talosconfigmanager.Configs,
	kubeconfigPath string,
	providerType v1alpha1.Provider,
	opts v1alpha1.OptionsTalos,
	hetznerOpts v1alpha1.OptionsHetzner,
	skipCNIChecks bool,
) (*TalosProvisioner, error) {
	// Validate or default the provider
	if providerType == "" {
		providerType = v1alpha1.ProviderDocker
	}

	// Create options and apply configured node counts
	talosconfigPath := opts.Config
	if talosconfigPath == "" {
		talosconfigPath = "~/.talos/config"
	}

	options := NewOptions().
		WithKubeconfigPath(kubeconfigPath).
		WithTalosconfigPath(talosconfigPath).
		WithSkipCNIChecks(skipCNIChecks)
	if opts.ControlPlanes > 0 {
		options.WithControlPlaneNodes(int(opts.ControlPlanes))
	}

	if opts.Workers > 0 {
		options.WithWorkerNodes(int(opts.Workers))
	}

	// Create provisioner with loaded configs and options
	provisioner := NewTalosProvisioner(talosConfigs, options)

	// Configure the infrastructure provider
	var infraProvider provider.Provider

	switch providerType {
	case v1alpha1.ProviderDocker:
		dockerClient, err := kindprovisioner.NewDefaultDockerClient()
		if err != nil {
			return nil, fmt.Errorf("failed to create Docker client: %w", err)
		}

		provisioner.WithDockerClient(dockerClient)
		infraProvider = docker.NewProvider(dockerClient, docker.LabelSchemeTalos)

	case v1alpha1.ProviderHetzner:
		hetznerProvider, err := createHetznerProvider(hetznerOpts)
		if err != nil {
			return nil, err
		}

		infraProvider = hetznerProvider
		// Store Hetzner options with defaults applied for cluster creation
		provisioner.WithHetznerOptions(applyHetznerDefaults(hetznerOpts))
		// Store Talos options with defaults applied (includes ISO)
		provisioner.WithTalosOptions(applyTalosDefaults(opts))

	default:
		return nil, fmt.Errorf("%w: %s (supported: %s, %s)",
			ErrUnsupportedProvider, providerType, v1alpha1.ProviderDocker, v1alpha1.ProviderHetzner)
	}

	provisioner.WithInfraProvider(infraProvider)

	return provisioner, nil
}

// createHetznerProvider creates a Hetzner provider from the given options.
func createHetznerProvider(opts v1alpha1.OptionsHetzner) (*hetzner.Provider, error) {
	// Determine the token environment variable name
	tokenEnvVar := opts.TokenEnvVar
	if tokenEnvVar == "" {
		tokenEnvVar = "HCLOUD_TOKEN"
	}

	// Get the token from the environment
	token := os.Getenv(tokenEnvVar)
	if token == "" {
		return nil, fmt.Errorf(
			"%w: environment variable %s is not set",
			ErrMissingHetznerToken,
			tokenEnvVar,
		)
	}

	// Create the Hetzner client and provider
	client := hcloud.NewClient(hcloud.WithToken(token))

	return hetzner.NewProvider(client), nil
}

// Hetzner default values - keep in sync with OptionsHetzner struct tags.
const (
	defaultHetznerServerType  = "cx23"
	defaultHetznerLocation    = "fsn1"
	defaultHetznerNetworkCIDR = "10.0.0.0/16"
	defaultHetznerTokenEnvVar = "HCLOUD_TOKEN"
)

// Talos default values - keep in sync with OptionsTalos struct tags.
const (
	defaultTalosISO = 122630 // Talos Linux 1.11.2 x86 (use 122629 for ARM)
)

// applyTalosDefaults applies default values to Talos options.
func applyTalosDefaults(opts v1alpha1.OptionsTalos) v1alpha1.OptionsTalos {
	if opts.ISO == 0 {
		opts.ISO = defaultTalosISO
	}

	return opts
}

// applyHetznerDefaults applies default values to Hetzner options.
func applyHetznerDefaults(opts v1alpha1.OptionsHetzner) v1alpha1.OptionsHetzner {
	if opts.ControlPlaneServerType == "" {
		opts.ControlPlaneServerType = defaultHetznerServerType
	}

	if opts.WorkerServerType == "" {
		opts.WorkerServerType = defaultHetznerServerType
	}

	if opts.Location == "" {
		opts.Location = defaultHetznerLocation
	}

	if opts.NetworkCIDR == "" {
		opts.NetworkCIDR = defaultHetznerNetworkCIDR
	}

	if opts.TokenEnvVar == "" {
		opts.TokenEnvVar = defaultHetznerTokenEnvVar
	}

	return opts
}
