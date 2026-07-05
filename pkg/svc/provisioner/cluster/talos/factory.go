package talosprovisioner

import (
	"fmt"
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	talosconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/docker"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/omni"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clustererr"
	kindprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/kind"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
)

// ErrUnsupportedProvider re-exports the shared error for backward compatibility.
var ErrUnsupportedProvider = clustererr.ErrUnsupportedProvider

// CreateProvisioner creates a Provisioner from a pre-loaded configuration.
// The Talos config should be loaded via the configmanager before calling this function,
// allowing any in-memory modifications (e.g., CNI patches, mirror registries) to be preserved.
//
// Parameters:
//   - talosConfigs: Pre-loaded Talos machine configurations with all patches applied
//   - kubeconfigPath: Path where the kubeconfig should be written
//   - kubeconfigContext: Desired kubeconfig context name (empty = derive from distribution)
//   - provider: Infrastructure provider (e.g., Docker, Hetzner, Omni)
//   - opts: Talos-specific options (node counts, etc.)
//   - hetznerOpts: Hetzner-specific options (required when provider is Hetzner)
//   - omniOpts: Omni-specific options (required when provider is Omni)
//   - skipCNIChecks: Whether to skip CNI-dependent checks (CoreDNS, kube-proxy) during bootstrap.
//     Set to true when KSail will install a custom CNI after cluster creation.
func CreateProvisioner(
	talosConfigs *talosconfigmanager.Configs,
	kubeconfigPath string,
	kubeconfigContext string,
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

	provisioner, provErr := newProvisionerFromOptions(
		talosConfigs,
		kubeconfigPath,
		kubeconfigContext,
		opts,
		skipCNIChecks,
	)
	if provErr != nil {
		return nil, provErr
	}

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
	kubeconfigContext string,
	opts v1alpha1.OptionsTalos,
	skipCNIChecks bool,
) (*Provisioner, error) {
	talosconfigPath := opts.Config
	if talosconfigPath == "" {
		talosconfigPath = "~/.talos/config"
	}

	options := NewOptions().
		WithKubeconfigPath(kubeconfigPath).
		WithKubeconfigContext(kubeconfigContext).
		WithTalosconfigPath(talosconfigPath).
		WithKubernetesVersion(opts.KubernetesVersion).
		WithDrainTimeout(opts.DrainTimeout.Duration).
		WithStorageHealthTimeout(opts.StorageHealthTimeout.Duration).
		WithSkipCNIChecks(skipCNIChecks)

	// Override the default Talos container image when a version pin is set.
	if version := strings.TrimSpace(opts.Version); version != "" {
		if !strings.HasPrefix(version, "v") {
			version = "v" + version
		}

		options.WithTalosImage(talosImageRepository + ":" + version)
	}

	//nolint:staticcheck // bridging deprecated field; consumed via overlay in factory.go
	if opts.ControlPlanes > 0 {
		//nolint:staticcheck // bridging deprecated field
		options.WithControlPlaneNodes(int(opts.ControlPlanes))
	}

	//nolint:staticcheck // bridging deprecated field; consumed via overlay in factory.go
	if opts.Workers > 0 {
		//nolint:staticcheck // bridging deprecated field
		options.WithWorkerNodes(int(opts.Workers))
	}

	if len(opts.ExtraPortMappings) > 0 {
		portStrings, portErr := PortMappingsToStrings(opts.ExtraPortMappings)
		if portErr != nil {
			return nil, fmt.Errorf("invalid port mappings: %w", portErr)
		}

		options.WithExtraPortMappings(portStrings)
	}

	return NewProvisioner(talosConfigs, options), nil
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
		hetznerProvider, hcloudClient, err := createHetznerProvider(hetznerOpts)
		if err != nil {
			return err
		}

		infraProvider = hetznerProvider
		// Store Hetzner options with defaults applied for cluster creation
		provisioner.WithHetznerOptions(applyHetznerDefaults(hetznerOpts))
		// Store Talos options with defaults applied (includes ISO)
		provisioner.WithTalosOptions(applyTalosDefaults(opts))

		// Wire the snapshot manager so EnsureTalosSnapshot / DeleteTalosSnapshots can be called.
		snapshotManager := hetzner.NewSnapshotManager(hcloudClient, provisioner.logWriter)
		provisioner.WithSnapshotManager(snapshotManager)

	case v1alpha1.ProviderOmni:
		omniProvider, err := createOmniProvider(omniOpts)
		if err != nil {
			return err
		}

		infraProvider = omniProvider
		// Store Omni options so the provisioner can route to Omni-specific logic
		provisioner.WithOmniOptions(omniOpts)

	case v1alpha1.ProviderAWS, v1alpha1.ProviderGCP, v1alpha1.ProviderAzure,
		v1alpha1.ProviderKubernetes:
		return unsupportedTalosProviderError(providerType)

	default:
		return fmt.Errorf("%w: %s (supported: %s, %s, %s)",
			ErrUnsupportedProvider, providerType,
			v1alpha1.ProviderDocker, v1alpha1.ProviderHetzner, v1alpha1.ProviderOmni)
	}

	provisioner.WithInfraProvider(infraProvider)

	return nil
}

// unsupportedTalosProviderError explains why a known-but-unsupported provider
// cannot back the Talos distribution, pointing at the supported alternative.
func unsupportedTalosProviderError(providerType v1alpha1.Provider) error {
	switch providerType {
	case v1alpha1.ProviderAWS:
		return fmt.Errorf("%w: %s (AWS is only supported with the EKS distribution)",
			ErrUnsupportedProvider, providerType)
	case v1alpha1.ProviderGCP:
		return fmt.Errorf("%w: %s (GCP is only supported with the GKE distribution)",
			ErrUnsupportedProvider, providerType)
	case v1alpha1.ProviderAzure:
		return fmt.Errorf("%w: %s (Azure is only supported with the AKS distribution)",
			ErrUnsupportedProvider, providerType)
	case v1alpha1.ProviderKubernetes:
		return fmt.Errorf("%w: %s (this factory does not accept Kubernetes provider; "+
			"use the Kubernetes-specific Talos provisioner instead)",
			ErrUnsupportedProvider, providerType)
	case v1alpha1.ProviderDocker, v1alpha1.ProviderHetzner, v1alpha1.ProviderOmni:
		// Supported providers never reach this helper.
		fallthrough
	default:
		return fmt.Errorf("%w: %s", ErrUnsupportedProvider, providerType)
	}
}

// createHetznerProvider creates a Hetzner provider and returns the underlying hcloud client.
// It delegates to the shared hetzner.NewProviderFromOptions for credential resolution so the
// configurable tokenEnvVar is honored consistently across all construction sites.
func createHetznerProvider(
	opts v1alpha1.OptionsHetzner,
) (*hetzner.Provider, *hcloud.Client, error) {
	prov, client, err := hetzner.NewProviderFromOptions(opts)
	if err != nil {
		return nil, nil, fmt.Errorf("create Hetzner provider: %w", err)
	}

	return prov, client, nil
}

// applyTalosDefaults applies default values to Talos options.
func applyTalosDefaults(opts v1alpha1.OptionsTalos) v1alpha1.OptionsTalos {
	if opts.ISO == 0 {
		opts.ISO = v1alpha1.DefaultTalosISO
	}

	return opts
}

// applyHetznerDefaults applies default values to Hetzner options.
func applyHetznerDefaults(opts v1alpha1.OptionsHetzner) v1alpha1.OptionsHetzner {
	if opts.ControlPlaneServerType == "" {
		opts.ControlPlaneServerType = v1alpha1.DefaultHetznerServerType
	}

	if opts.WorkerServerType == "" {
		opts.WorkerServerType = v1alpha1.DefaultHetznerServerType
	}

	if opts.Location == "" {
		opts.Location = v1alpha1.DefaultHetznerLocation
	}

	if opts.NetworkCIDR == "" {
		opts.NetworkCIDR = v1alpha1.DefaultHetznerNetworkCIDR
	}

	if opts.TokenEnvVar == "" {
		opts.TokenEnvVar = v1alpha1.DefaultHetznerTokenEnvVar
	}

	if len(opts.FallbackLocations) == 0 {
		opts.FallbackLocations = v1alpha1.DefaultHetznerFallbackLocations()
	}

	// The floating IP is homed in the cluster's location unless explicitly
	// overridden (homing affects routing latency only). Location is already
	// defaulted above, so this always resolves to a concrete location.
	if opts.FloatingIPLocation == "" {
		opts.FloatingIPLocation = opts.Location
	}

	return opts
}

// createOmniProvider creates an Omni provider from the given options.
// Delegates to the shared omni.NewProviderFromOptions for credential resolution.
func createOmniProvider(opts v1alpha1.OptionsOmni) (*omni.Provider, error) {
	prov, err := omni.NewProviderFromOptions(opts)
	if err != nil {
		return nil, fmt.Errorf("create Omni provider: %w", err)
	}

	return prov, nil
}
