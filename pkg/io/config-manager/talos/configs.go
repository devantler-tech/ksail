package talos

import (
	"fmt"
	"net"
	"net/netip"

	talosconfig "github.com/siderolabs/talos/pkg/machinery/config"
	"github.com/siderolabs/talos/pkg/machinery/config/bundle"
	"github.com/siderolabs/talos/pkg/machinery/config/generate"
	"github.com/siderolabs/talos/pkg/machinery/config/types/v1alpha1"
)

// Configs holds the loaded Talos machine configurations with patches applied.
// It wraps the upstream bundle.Bundle and provides convenient accessors.
//
// Usage:
//
//	manager := NewConfigManager("talos", "my-cluster", "1.32.0", "10.5.0.0/24")
//	configs, err := manager.LoadConfig(nil)
//	if err != nil {
//	    return err
//	}
//
//	// Access programmatically
//	cpConfig := configs.ControlPlane()
//	workerConfig := configs.Worker()
//
//	// Access specific config sections
//	cniName := cpConfig.Cluster().Network().CNI().Name()
//	kubeletImage := cpConfig.Machine().Kubelet().Image()
type Configs struct {
	// Name is the cluster name.
	Name string
	// bundle is the underlying Talos config bundle.
	bundle *bundle.Bundle
}

// Bundle returns the underlying Talos config bundle.
// This provides full access to all bundle functionality.
func (c *Configs) Bundle() *bundle.Bundle {
	return c.bundle
}

// ControlPlane returns the control-plane machine configuration.
// This config has cluster and control-plane patches applied.
//
// The returned config.Provider gives programmatic access to all config fields:
//   - Machine() - machine-specific settings (network, kubelet, files, etc.)
//   - Cluster() - cluster-wide settings (CNI, API server, etcd, etc.)
//
//nolint:ireturn // Returns interface type from upstream SDK
func (c *Configs) ControlPlane() talosconfig.Provider {
	return c.bundle.ControlPlane()
}

// Worker returns the worker machine configuration.
// This config has cluster and worker patches applied.
//
// The returned config.Provider gives programmatic access to all config fields:
//   - Machine() - machine-specific settings (network, kubelet, files, etc.)
//   - Cluster() - cluster-wide settings (CNI, API server, etcd, etc.)
//
//nolint:ireturn // Returns interface type from upstream SDK
func (c *Configs) Worker() talosconfig.Provider {
	return c.bundle.Worker()
}

// GetClusterName returns the cluster name.
// This implements configmanager.ClusterNameProvider interface.
func (c *Configs) GetClusterName() string {
	return c.Name
}

// IsCNIDisabled returns true if the default CNI is disabled (set to "none").
// This is used to determine whether to skip CNI-dependent checks during bootstrap.
func (c *Configs) IsCNIDisabled() bool {
	cp := c.ControlPlane()
	if cp == nil || cp.Cluster() == nil || cp.Cluster().Network() == nil {
		return false
	}

	cni := cp.Cluster().Network().CNI()
	if cni == nil {
		return false
	}

	return cni.Name() == "none"
}

// NetworkCIDR returns the network CIDR from the cluster configuration.
// This is extracted from the pod CIDRs in the cluster network settings.
func (c *Configs) NetworkCIDR() string {
	cp := c.ControlPlane()
	if cp == nil || cp.Cluster() == nil || cp.Cluster().Network() == nil {
		return DefaultNetworkCIDR
	}

	podCIDRs := cp.Cluster().Network().PodCIDRs()
	if len(podCIDRs) > 0 {
		return podCIDRs[0]
	}

	return DefaultNetworkCIDR
}

// newConfigs creates Configs from patches with the given cluster parameters.
func newConfigs(
	clusterName string,
	kubernetesVersion string,
	networkCIDR string,
	patches []Patch,
) (*Configs, error) {
	// Categorize patches by scope
	clusterPatches, controlPlanePatches, workerPatches, err := categorizePatchesByScope(patches)
	if err != nil {
		return nil, err
	}

	// Parse network CIDR for endpoint calculation
	parsedCIDR, err := netip.ParsePrefix(networkCIDR)
	if err != nil {
		return nil, fmt.Errorf("invalid network CIDR: %w", err)
	}

	// Calculate control plane IP for endpoint
	controlPlaneIP, err := nthIPInNetwork(parsedCIDR, ipv4Offset)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate control plane IP: %w", err)
	}

	controlPlaneEndpoint := "https://" + net.JoinHostPort(controlPlaneIP.String(), "6443")

	// Build generate options with endpoint list and localhost SAN for Docker-in-VM environments
	genOptions := []generate.Option{
		generate.WithEndpointList([]string{controlPlaneIP.String()}),
		generate.WithAdditionalSubjectAltNames([]string{"127.0.0.1"}),
	}

	// Build bundle options
	bundleOpts := []bundle.Option{
		bundle.WithInputOptions(&bundle.InputOptions{
			ClusterName: clusterName,
			Endpoint:    controlPlaneEndpoint,
			KubeVersion: kubernetesVersion,
			GenOptions:  genOptions,
		}),
		bundle.WithVerbose(false), // Suppress "generating PKI and tokens" output
	}

	// Add patches by scope
	if len(clusterPatches) > 0 {
		bundleOpts = append(bundleOpts, bundle.WithPatch(clusterPatches))
	}

	if len(controlPlanePatches) > 0 {
		bundleOpts = append(bundleOpts, bundle.WithPatchControlPlane(controlPlanePatches))
	}

	if len(workerPatches) > 0 {
		bundleOpts = append(bundleOpts, bundle.WithPatchWorker(workerPatches))
	}

	// Create the bundle
	configBundle, err := bundle.NewBundle(bundleOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create config bundle: %w", err)
	}

	return &Configs{Name: clusterName, bundle: configBundle}, nil
}

// MirrorRegistry represents a registry mirror configuration.
type MirrorRegistry struct {
	// Host is the registry host (e.g., "docker.io", "ghcr.io").
	Host string
	// Endpoints are the mirror endpoints to use (e.g., "http://docker.io:5000").
	Endpoints []string
}

// ApplyMirrorRegistries modifies the configs to add registry mirror configurations.
// This directly patches the underlying v1alpha1.Config structs.
// It adds both the mirror endpoints and the registry config with insecureSkipVerify: true
// to allow HTTP connections to local registry containers.
func (c *Configs) ApplyMirrorRegistries(mirrors []MirrorRegistry) error {
	if len(mirrors) == 0 {
		return nil
	}

	// Define the patcher function that adds mirrors and registry configs
	patcher := func(cfg *v1alpha1.Config) error {
		return applyMirrorsToConfig(cfg, mirrors)
	}

	// Apply to control plane config
	if c.bundle.ControlPlaneCfg != nil {
		patched, err := c.bundle.ControlPlaneCfg.PatchV1Alpha1(patcher)
		if err != nil {
			return fmt.Errorf("failed to patch control plane config: %w", err)
		}

		c.bundle.ControlPlaneCfg = patched
	}

	// Apply to worker config
	if c.bundle.WorkerCfg != nil {
		patched, err := c.bundle.WorkerCfg.PatchV1Alpha1(patcher)
		if err != nil {
			return fmt.Errorf("failed to patch worker config: %w", err)
		}

		c.bundle.WorkerCfg = patched
	}

	return nil
}

// applyMirrorsToConfig applies mirror configurations to a Talos v1alpha1 config.
func applyMirrorsToConfig(cfg *v1alpha1.Config, mirrors []MirrorRegistry) error {
	if cfg.MachineConfig == nil {
		return nil
	}

	initRegistryMaps(cfg)

	for _, mirror := range mirrors {
		if mirror.Host == "" {
			continue
		}

		addMirrorEndpoints(cfg, mirror)
		addInsecureRegistryConfigs(cfg, mirror.Endpoints)
	}

	return nil
}

// initRegistryMaps initializes the registry maps if they are nil.
//
//nolint:staticcheck // MachineRegistries is deprecated but still functional in Talos v1.x
func initRegistryMaps(cfg *v1alpha1.Config) {
	if cfg.MachineConfig.MachineRegistries.RegistryMirrors == nil {
		cfg.MachineConfig.MachineRegistries.RegistryMirrors = make(
			map[string]*v1alpha1.RegistryMirrorConfig,
		)
	}

	if cfg.MachineConfig.MachineRegistries.RegistryConfig == nil {
		cfg.MachineConfig.MachineRegistries.RegistryConfig = make(
			map[string]*v1alpha1.RegistryConfig,
		)
	}
}

// addMirrorEndpoints adds mirror endpoint configuration for a registry host.
//
//nolint:staticcheck // MachineRegistries is deprecated but still functional in Talos v1.x
func addMirrorEndpoints(cfg *v1alpha1.Config, mirror MirrorRegistry) {
	cfg.MachineConfig.MachineRegistries.RegistryMirrors[mirror.Host] = &v1alpha1.RegistryMirrorConfig{
		MirrorEndpoints: mirror.Endpoints,
	}
}

// addInsecureRegistryConfigs adds insecureSkipVerify config for HTTP endpoints.
//
//nolint:staticcheck // MachineRegistries is deprecated but still functional in Talos v1.x
func addInsecureRegistryConfigs(cfg *v1alpha1.Config, endpoints []string) {
	for _, endpoint := range endpoints {
		if !isHTTPEndpoint(endpoint) {
			continue
		}

		configKey := extractHostPortFromEndpoint(endpoint)
		if configKey == "" {
			continue
		}

		insecureSkipVerify := true
		cfg.MachineConfig.MachineRegistries.RegistryConfig[configKey] = &v1alpha1.RegistryConfig{
			RegistryTLS: &v1alpha1.RegistryTLSConfig{
				TLSInsecureSkipVerify: &insecureSkipVerify,
			},
		}
	}
}

// extractHostPortFromEndpoint extracts the host:port portion from an endpoint URL.
// For example, "http://docker.io:5000" returns "docker.io:5000".
func extractHostPortFromEndpoint(endpoint string) string {
	// Remove scheme (http:// or https://)
	hostPort := endpoint
	if idx := len("http://"); len(endpoint) > idx && endpoint[:idx] == "http://" {
		hostPort = endpoint[idx:]
	} else if idx := len("https://"); len(endpoint) > idx && endpoint[:idx] == "https://" {
		hostPort = endpoint[idx:]
	}

	// Remove any path component
	if slashIdx := indexByte(hostPort, '/'); slashIdx >= 0 {
		hostPort = hostPort[:slashIdx]
	}

	return hostPort
}

// isHTTPEndpoint returns true if the endpoint uses HTTP (not HTTPS).
func isHTTPEndpoint(endpoint string) bool {
	return len(endpoint) >= 7 && endpoint[:7] == "http://"
}

// indexByte returns the index of the first occurrence of c in s, or -1 if not present.
func indexByte(s string, c byte) int {
	for i := range len(s) {
		if s[i] == c {
			return i
		}
	}

	return -1
}
