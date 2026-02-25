package talosprovisioner

import (
	"errors"
	"fmt"
	"os"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	talosconfigmanager "github.com/devantler-tech/ksail/v5/pkg/fsutil/configmanager/talos"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provider"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provider/docker"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provider/hetzner"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provider/omni"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/clustererr"
	kindprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/kind"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	omniclient "github.com/siderolabs/omni/client/pkg/client"
)

// ErrUnsupportedProvider re-exports the shared error for backward compatibility.
var ErrUnsupportedProvider = clustererr.ErrUnsupportedProvider

// ErrMissingHetznerToken is returned when the Hetzner API token is not set.
var ErrMissingHetznerToken = errors.New("hetzner API token not set")

// CreateProvisioner creates a Provisioner from a pre-loaded configuration.
// The Talos config should be loaded via the configmanager before calling this function,
// allowing any in-memory modifications (e.g., CNI patches, mirror registries) to be preserved.
//
// Parameters:
//   - talosConfigs: Pre-loaded Talos machine configurations with all patches applied
//   - kubeconfigPath: Path where the kubeconfig should be written
//   - provider: Infrastructure provider (e.g., Docker, Hetzner, Omni)
//   - opts: Talos-specific options (node counts, etc.)
//   - hetznerOpts: Hetzner-specific options (required when provider is Hetzner)
//   - omniOpts: Omni-specific options (required when provider is Omni)
//   - skipCNIChecks: Whether to skip CNI-dependent checks (CoreDNS, kube-proxy) during bootstrap.
//     Set to true when KSail will install a custom CNI after cluster creation.
func CreateProvisioner(
	talosConfigs *talosconfigmanager.Configs,
	kubeconfigPath string,
	providerType v1alpha1.Provider,
	opts v1alpha1.OptionsTalos,
	hetznerOpts v1alpha1.OptionsHetzner,
	omniOpts v1alpha1.OptionsOmni,
	skipCNIChecks bool,
) (*Provisioner, error) {
	// Validate or default the provider
	if providerType == "" {
		providerType = v1alpha1.ProviderDocker
	}

	provisioner := newProvisionerFromOptions(talosConfigs, kubeconfigPath, opts, skipCNIChecks)

	// Configure the infrastructure provider
	err := configureInfraProvider(
		provisioner, providerType, opts, hetznerOpts, omniOpts,
	)
	if err != nil {
		return nil, err
	}

	return provisioner, nil
}

// newProvisionerFromOptions creates a provisioner with the given options applied.
func newProvisionerFromOptions(
	talosConfigs *talosconfigmanager.Configs,
	kubeconfigPath string,
	opts v1alpha1.OptionsTalos,
	skipCNIChecks bool,
) *Provisioner {
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

	return NewProvisioner(talosConfigs, options)
}

// configureInfraProvider configures the infrastructure provider on the provisioner.
func configureInfraProvider(
	provisioner *Provisioner,
	providerType v1alpha1.Provider,
	opts v1alpha1.OptionsTalos,
	hetznerOpts v1alpha1.OptionsHetzner,
	omniOpts v1alpha1.OptionsOmni,
) error {
	var infraProvider provider.Provider

	switch providerType {
	case v1alpha1.ProviderDocker:
		dockerClient, err := kindprovisioner.NewDefaultDockerClient()
		if err != nil {
			return fmt.Errorf("failed to create Docker client: %w", err)
		}

		provisioner.WithDockerClient(dockerClient)
		infraProvider = docker.NewProvider(dockerClient, docker.LabelSchemeTalos)

	case v1alpha1.ProviderHetzner:
		hetznerProvider, err := createHetznerProvider(hetznerOpts)
		if err != nil {
			return err
		}

		infraProvider = hetznerProvider
		// Store Hetzner options with defaults applied for cluster creation
		provisioner.WithHetznerOptions(applyHetznerDefaults(hetznerOpts))
		// Store Talos options with defaults applied (includes ISO)
		provisioner.WithTalosOptions(applyTalosDefaults(opts))

	case v1alpha1.ProviderOmni:
		omniProvider, err := createOmniProvider(omniOpts)
		if err != nil {
			return err
		}

		infraProvider = omniProvider

	default:
		return fmt.Errorf("%w: %s (supported: %s, %s, %s)",
			ErrUnsupportedProvider, providerType,
			v1alpha1.ProviderDocker, v1alpha1.ProviderHetzner, v1alpha1.ProviderOmni)
	}

	provisioner.WithInfraProvider(infraProvider)

	return nil
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

// Omni default values - keep in sync with OptionsOmni struct tags.
const (
	// defaultOmniServiceAccountKeyEnvVar is the default environment variable
	// name for the Omni service account key used for API authentication.
	defaultOmniServiceAccountKeyEnvVar = "OMNI_SERVICE_ACCOUNT_KEY"
)

// createOmniProvider creates an Omni provider from the given options.
func createOmniProvider(opts v1alpha1.OptionsOmni) (*omni.Provider, error) {
	if opts.Endpoint == "" {
		return nil, omni.ErrEndpointRequired
	}

	// Determine the service account key environment variable name
	keyEnvVar := opts.ServiceAccountKeyEnvVar
	if keyEnvVar == "" {
		keyEnvVar = defaultOmniServiceAccountKeyEnvVar
	}

	// Get the service account key from the environment
	serviceAccountKey := os.Getenv(keyEnvVar)
	if serviceAccountKey == "" {
		return nil, fmt.Errorf(
			"%w: environment variable %s is not set",
			omni.ErrServiceAccountKeyRequired,
			keyEnvVar,
		)
	}

	// Create the Omni client and provider
	client, err := omniclient.New(
		opts.Endpoint,
		omniclient.WithServiceAccount(serviceAccountKey),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create Omni client: %w", err)
	}

	return omni.NewProvider(client), nil
}
