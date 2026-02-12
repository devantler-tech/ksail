package scaffolder

import (
	"fmt"
	"net"
	"path/filepath"
	"strconv"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	dockerclient "github.com/devantler-tech/ksail/v5/pkg/client/docker"
	k3dconfigmanager "github.com/devantler-tech/ksail/v5/pkg/fsutil/configmanager/k3d"
	yamlgenerator "github.com/devantler-tech/ksail/v5/pkg/fsutil/generator/yaml"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/registry"
	"github.com/k3d-io/k3d/v5/pkg/config/types"
	k3dv1alpha5 "github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
)

// GenerateK3dRegistryConfig generates K3d registry configuration for mirror registry.
// Input format: "name=upstream" (e.g., "docker.io=https://registry-1.docker.io")
// K3d requires one registry per proxy, so we generate multiple create configs.
// Registry containers are prefixed with the cluster name to avoid Docker DNS collisions.
func (s *Scaffolder) GenerateK3dRegistryConfig() k3dv1alpha5.SimpleConfigRegistries {
	registryConfig := k3dv1alpha5.SimpleConfigRegistries{}

	if s.KSailConfig.Spec.Cluster.Distribution != v1alpha1.DistributionK3s {
		return registryConfig
	}

	specs := registry.ParseMirrorSpecs(s.MirrorRegistries)

	// Resolve cluster name for registry container prefixing.
	// This ensures registry containers are named like "k3d-ghcr.io" instead of "ghcr.io"
	// to avoid Docker DNS collisions when running multiple clusters.
	clusterName := k3dconfigmanager.ResolveClusterName(&s.KSailConfig, nil)

	hostEndpoints, updated := registry.BuildHostEndpointMap(specs, clusterName, nil)
	if len(hostEndpoints) == 0 || !updated {
		return registryConfig
	}

	registryConfig.Config = registry.RenderK3dMirrorConfig(hostEndpoints)

	return registryConfig
}

// CreateK3dConfig creates a K3d configuration with distribution-specific settings.
// Node counts can be set via --control-planes and --workers CLI flags.
func (s *Scaffolder) CreateK3dConfig() k3dv1alpha5.SimpleConfig {
	// Resolve cluster name - use explicit ClusterName if set, otherwise resolve from config
	var clusterName string
	if s.ClusterName != "" {
		clusterName = s.ClusterName
	} else {
		clusterName = k3dconfigmanager.ResolveClusterName(&s.KSailConfig, nil)
	}

	config := k3dv1alpha5.SimpleConfig{
		TypeMeta: types.TypeMeta{
			APIVersion: "k3d.io/v1alpha5",
			Kind:       "Simple",
		},
		ObjectMeta: types.ObjectMeta{
			Name: clusterName,
		},
	}

	// Apply node counts from CLI flags (stored in Talos options)
	// These values are used across all distributions for consistency
	controlPlanes := int(s.KSailConfig.Spec.Cluster.Talos.ControlPlanes)
	workers := int(s.KSailConfig.Spec.Cluster.Talos.Workers)

	if controlPlanes > 0 {
		config.Servers = controlPlanes
	}

	if workers > 0 {
		config.Agents = workers
	}

	if extraArgs := s.buildK3dExtraArgs(); len(extraArgs) > 0 {
		config.Options.K3sOptions.ExtraArgs = extraArgs
	}

	// Add registry configuration for mirror registries
	if len(s.MirrorRegistries) > 0 {
		config.Registries = s.GenerateK3dRegistryConfig()
	}

	// Configure K3d-native local registry when enabled.
	// K3d's Registries.Create automatically manages the registry container,
	// including DNS resolution, network connectivity, and lifecycle management.
	if s.KSailConfig.Spec.Cluster.LocalRegistry.Enabled() {
		config.Registries = s.addK3dLocalRegistryConfig(config.Registries)
	}

	return config
}

// buildK3dExtraArgs constructs K3s server arguments that disable built-in
// components when the KSail configuration specifies alternative or disabled values.
func (s *Scaffolder) buildK3dExtraArgs() []k3dv1alpha5.K3sArgWithNodeFilters {
	var extraArgs []k3dv1alpha5.K3sArgWithNodeFilters

	// Disable default CNI (Flannel) if using a non-default CNI (Cilium or Calico)
	if s.KSailConfig.Spec.Cluster.CNI == v1alpha1.CNICilium ||
		s.KSailConfig.Spec.Cluster.CNI == v1alpha1.CNICalico {
		extraArgs = append(extraArgs,
			k3dv1alpha5.K3sArgWithNodeFilters{
				Arg:         "--flannel-backend=none",
				NodeFilters: []string{"server:*"},
			},
			k3dv1alpha5.K3sArgWithNodeFilters{
				Arg:         "--disable-network-policy",
				NodeFilters: []string{"server:*"},
			},
		)
	}

	// Disable metrics-server if explicitly disabled (K3s includes it by default)
	if s.KSailConfig.Spec.Cluster.MetricsServer == v1alpha1.MetricsServerDisabled {
		extraArgs = append(extraArgs,
			k3dv1alpha5.K3sArgWithNodeFilters{
				Arg:         "--disable=metrics-server",
				NodeFilters: []string{"server:*"},
			},
		)
	}

	// Disable local-storage if explicitly disabled (K3s includes it by default)
	if s.KSailConfig.Spec.Cluster.CSI == v1alpha1.CSIDisabled {
		extraArgs = append(extraArgs,
			k3dv1alpha5.K3sArgWithNodeFilters{
				Arg:         "--disable=local-storage",
				NodeFilters: []string{"server:*"},
			},
		)
	}

	// Disable servicelb if explicitly disabled (K3s includes it by default)
	if s.KSailConfig.Spec.Cluster.LoadBalancer == v1alpha1.LoadBalancerDisabled {
		extraArgs = append(extraArgs,
			k3dv1alpha5.K3sArgWithNodeFilters{
				Arg:         "--disable=servicelb",
				NodeFilters: []string{"server:*"},
			},
		)
	}

	return extraArgs
}

// addK3dLocalRegistryConfig adds K3d-native local registry configuration.
// K3d's Registries.Create automatically manages the registry container,
// including DNS resolution, network connectivity, and lifecycle management.
func (s *Scaffolder) addK3dLocalRegistryConfig(
	registryConfig k3dv1alpha5.SimpleConfigRegistries,
) k3dv1alpha5.SimpleConfigRegistries {
	// Resolve cluster name to build the registry container name.
	// K3d creates the registry with the exact name specified.
	clusterName := k3dconfigmanager.ResolveClusterName(&s.KSailConfig, nil)
	registryName := registry.BuildLocalRegistryName(clusterName)

	// Determine the host port from config or use default
	hostPort := s.KSailConfig.Spec.Cluster.LocalRegistry.ResolvedPort()

	// Configure K3d to create and manage the local registry.
	// K3d will create a registry container named "k3d-<registryName>"
	registryConfig.Create = &k3dv1alpha5.SimpleConfigRegistryCreateConfig{
		Name:     registryName,
		Host:     dockerclient.RegistryHostIP,
		HostPort: strconv.FormatInt(int64(hostPort), 10),
	}

	// Also configure the containerd mirror so nodes can pull images.
	// K3d Registries.Create uses the name directly (without k3d- prefix).
	registryHost := net.JoinHostPort(
		registryName,
		strconv.Itoa(dockerclient.DefaultRegistryPort),
	)
	registryEndpoint := "http://" + registryHost

	// Parse existing config and add local registry endpoint
	hostEndpoints := k3dconfigmanager.ParseRegistryConfig(registryConfig.Config)
	if hostEndpoints == nil {
		hostEndpoints = make(map[string][]string)
	}

	hostEndpoints[registryHost] = []string{registryEndpoint}

	registryConfig.Config = registry.RenderK3dMirrorConfig(hostEndpoints)

	return registryConfig
}

// generateK3dConfig generates the k3d.yaml configuration file.
func (s *Scaffolder) generateK3dConfig(output string, force bool) error {
	k3dConfig := s.CreateK3dConfig()

	opts := yamlgenerator.Options{
		Output: filepath.Join(output, "k3d.yaml"),
		Force:  force,
	}

	return generateWithFileHandling(
		s,
		GenerationParams[*k3dv1alpha5.SimpleConfig]{
			Gen:         s.K3dGenerator,
			Model:       &k3dConfig,
			Opts:        opts,
			DisplayName: "k3d.yaml",
			Force:       force,
			WrapErr: func(err error) error {
				return fmt.Errorf("%w: %w", ErrK3dConfigGeneration, err)
			},
		},
	)
}
