package talos

import (
	"fmt"
	"net"
	"net/netip"
	"time"

	talosconfig "github.com/siderolabs/talos/pkg/machinery/config"
	"github.com/siderolabs/talos/pkg/machinery/config/bundle"
	"github.com/siderolabs/talos/pkg/machinery/config/configpatcher"
	"github.com/siderolabs/talos/pkg/machinery/config/generate"
	"github.com/siderolabs/talos/pkg/machinery/config/generate/secrets"
	"github.com/siderolabs/talos/pkg/machinery/config/types/v1alpha1"
)

// Configs holds the loaded Talos machine configurations with patches applied.
// It wraps the upstream bundle.Bundle and provides convenient accessors.
//
// Usage:
//
//	manager := NewConfigManager("talos", "my-cluster", "1.32.0", "10.5.0.0/24")
//	configs, err := manager.Load(configmanager.LoadOptions{})
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
	// kubernetesVersion is stored for regeneration with a new name.
	kubernetesVersion string
	// networkCIDR is stored for regeneration with a new name.
	networkCIDR string
	// endpoint is an optional override endpoint IP for cloud deployments (e.g., Hetzner public IP).
	// When empty, the endpoint is calculated from networkCIDR.
	endpoint string
	// patches is stored for regeneration with a new name.
	patches []Patch
}

// NewDefaultConfigs creates a new Talos Configs with default settings.
// This is used when no scaffolded project exists and default configurations are needed.
// It creates a valid config bundle with:
//   - Cluster name: DefaultClusterName ("talos-default")
//   - Kubernetes version: DefaultKubernetesVersion ("1.32.0")
//   - Network CIDR: DefaultNetworkCIDR ("10.5.0.0/24")
//   - allowSchedulingOnControlPlanes: true (for single-node/control-plane-only clusters)
func NewDefaultConfigs() (*Configs, error) {
	// Default configs are used for control-plane-only clusters (no workers),
	// so we need to allow scheduling on control-plane nodes.
	allowSchedulingPatch := Patch{
		Path:    "allow-scheduling-on-control-planes",
		Scope:   PatchScopeCluster,
		Content: []byte("cluster:\n  allowSchedulingOnControlPlanes: true\n"),
	}

	return newConfigs(
		DefaultClusterName,
		DefaultKubernetesVersion,
		DefaultNetworkCIDR,
		[]Patch{allowSchedulingPatch},
	)
}

// NewDefaultConfigsWithPatches creates a new Talos Configs with default settings plus additional patches.
// This is used when no scaffolded project exists but additional runtime patches are needed
// (e.g., kubelet-csr-approver extraManifests when metrics-server is enabled).
//
// The additional patches are applied after the default allowSchedulingOnControlPlanes patch.
func NewDefaultConfigsWithPatches(additionalPatches []Patch) (*Configs, error) {
	// Default configs are used for control-plane-only clusters (no workers),
	// so we need to allow scheduling on control-plane nodes.
	allowSchedulingPatch := Patch{
		Path:    "allow-scheduling-on-control-planes",
		Scope:   PatchScopeCluster,
		Content: []byte("cluster:\n  allowSchedulingOnControlPlanes: true\n"),
	}

	patches := make([]Patch, 0, 1+len(additionalPatches))
	patches = append(patches, allowSchedulingPatch)
	patches = append(patches, additionalPatches...)

	return newConfigs(
		DefaultClusterName,
		DefaultKubernetesVersion,
		DefaultNetworkCIDR,
		patches,
	)
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
// Returns nil if the bundle is not loaded or if the control plane config is not set.
// This prevents panics from the upstream Talos SDK's bundle.ControlPlane() method
// which panics when ControlPlaneCfg is nil.
//
//nolint:ireturn // Returns interface type from upstream SDK
func (c *Configs) ControlPlane() talosconfig.Provider {
	if c.bundle == nil || c.bundle.ControlPlaneCfg == nil {
		return nil
	}

	return c.bundle.ControlPlane()
}

// Worker returns the worker machine configuration.
// This config has cluster and worker patches applied.
//
// The returned config.Provider gives programmatic access to all config fields:
//   - Machine() - machine-specific settings (network, kubelet, files, etc.)
//   - Cluster() - cluster-wide settings (CNI, API server, etcd, etc.)
//
// Returns nil if the bundle is not loaded or if the worker config is not set.
// This prevents panics from the upstream Talos SDK's bundle.Worker() method
// which panics when WorkerCfg is nil.
//
//nolint:ireturn // Returns interface type from upstream SDK
func (c *Configs) Worker() talosconfig.Provider {
	if c.bundle == nil || c.bundle.WorkerCfg == nil {
		return nil
	}

	return c.bundle.Worker()
}

// GetClusterName returns the cluster name.
// This implements configmanager.ClusterNameProvider interface.
func (c *Configs) GetClusterName() string {
	return c.Name
}

// WithName creates a new Configs with a different cluster name.
// This regenerates the underlying Talos bundle with the new cluster name,
// which is necessary because the cluster name is embedded in PKI certificates
// and the kubeconfig context name (admin@<cluster-name>).
//
// Returns a new Configs instance; the original is not modified.
// Returns an error if bundle regeneration fails.
func (c *Configs) WithName(name string) (*Configs, error) {
	if name == "" || name == c.Name {
		return c, nil
	}

	// Use stored values for regeneration, falling back to defaults
	kubernetesVersion := c.kubernetesVersion
	if kubernetesVersion == "" {
		kubernetesVersion = DefaultKubernetesVersion
	}

	networkCIDR := c.networkCIDR
	if networkCIDR == "" {
		networkCIDR = DefaultNetworkCIDR
	}

	// Regenerate the bundle with the new cluster name
	return newConfigsWithEndpoint(name, kubernetesVersion, networkCIDR, c.endpoint, c.patches)
}

// WithEndpoint creates a new Configs with a specific endpoint IP for the Talos API and Kubernetes API.
// This is used for cloud deployments (e.g., Hetzner) where the public IP is different from
// the internal network IP. The endpoint is embedded in certificates and must match the IP
// that clients will use to connect.
//
// The endpoint should be the public IP address of the first control plane node.
// Returns a new Configs instance; the original is not modified.
// Returns an error if bundle regeneration fails.
//
// IMPORTANT: This preserves the existing PKI (CA, certificates) to ensure that configs
// applied to servers and the talosconfig for authentication use the same CA.
func (c *Configs) WithEndpoint(endpointIP string) (*Configs, error) {
	if endpointIP == "" || endpointIP == c.endpoint {
		return c, nil
	}

	// Use stored values for regeneration, falling back to defaults
	kubernetesVersion := c.kubernetesVersion
	if kubernetesVersion == "" {
		kubernetesVersion = DefaultKubernetesVersion
	}

	networkCIDR := c.networkCIDR
	if networkCIDR == "" {
		networkCIDR = DefaultNetworkCIDR
	}

	// Extract existing secrets bundle to preserve PKI across endpoint changes
	var existingSecrets *secrets.Bundle
	if c.bundle != nil && c.bundle.ControlPlaneCfg != nil {
		existingSecrets = secrets.NewBundleFromConfig(
			secrets.NewFixedClock(time.Now()),
			c.bundle.ControlPlaneCfg,
		)
	}

	// Regenerate the bundle with the new endpoint but preserved secrets
	return newConfigsWithEndpointAndSecrets(
		c.Name,
		kubernetesVersion,
		networkCIDR,
		endpointIP,
		c.patches,
		existingSecrets,
	)
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

// IsKubeletCertRotationEnabled returns true if kubelet serving certificate rotation is enabled.
// This is detected by the presence of "rotate-server-certificates" in kubelet extra args.
// When enabled with CNI disabled, the kubelet-serving-cert-approver pod cannot schedule
// (node is NotReady without CNI), so kubelet has no serving certificate, and Talos cannot
// connect to kubelet to populate StaticPodStatus resources.
func (c *Configs) IsKubeletCertRotationEnabled() bool {
	cp := c.ControlPlane()
	if cp == nil || cp.Machine() == nil || cp.Machine().Kubelet() == nil {
		return false
	}

	extraArgs := cp.Machine().Kubelet().ExtraArgs()
	if extraArgs == nil {
		return false
	}

	val, ok := extraArgs["rotate-server-certificates"]

	return ok && val == "true"
}

// ExtractMirrorHosts returns a list of registry hosts that have mirror configurations.
// This extracts hosts from the loaded config bundle, which includes any patches that
// were applied (including scaffolded mirror-registries.yaml patches).
// Returns nil if no mirrors are configured.
//
// Note: This method only returns host names, not remote URLs. For full MirrorSpec
// extraction including remotes, parsing from the generator-created patch files is
// needed, or use DefaultGenerateUpstreamURL to derive conventional upstream URLs.
func (c *Configs) ExtractMirrorHosts() []string {
	cp := c.ControlPlane()
	if cp == nil {
		return nil
	}

	mirrors := cp.RegistryMirrorConfigs()
	if len(mirrors) == 0 {
		return nil
	}

	hosts := make([]string, 0, len(mirrors))
	for host := range mirrors {
		hosts = append(hosts, host)
	}

	return hosts
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
// The endpoint is calculated from the network CIDR.
func newConfigs(
	clusterName string,
	kubernetesVersion string,
	networkCIDR string,
	patches []Patch,
) (*Configs, error) {
	return newConfigsWithEndpointAndSecrets(
		clusterName,
		kubernetesVersion,
		networkCIDR,
		"",
		patches,
		nil,
	)
}

// newConfigsWithEndpoint creates Configs with an optional explicit endpoint IP.
// If endpointIP is empty, the endpoint is calculated from the network CIDR.
// If endpointIP is provided (e.g., for Hetzner public IPs), it is used as the endpoint.
func newConfigsWithEndpoint(
	clusterName string,
	kubernetesVersion string,
	networkCIDR string,
	endpointIP string,
	patches []Patch,
) (*Configs, error) {
	return newConfigsWithEndpointAndSecrets(
		clusterName,
		kubernetesVersion,
		networkCIDR,
		endpointIP,
		patches,
		nil,
	)
}

// resolveControlPlaneIP determines the control plane IP from explicit endpoint or network CIDR.
func resolveControlPlaneIP(endpointIP, networkCIDR string) (string, error) {
	if endpointIP != "" {
		return endpointIP, nil
	}

	// Parse network CIDR for endpoint calculation
	parsedCIDR, parseErr := netip.ParsePrefix(networkCIDR)
	if parseErr != nil {
		return "", fmt.Errorf("invalid network CIDR: %w", parseErr)
	}

	// Calculate control plane IP for endpoint
	ipAddr, ipErr := nthIPInNetwork(parsedCIDR, ipv4Offset)
	if ipErr != nil {
		return "", fmt.Errorf("failed to calculate control plane IP: %w", ipErr)
	}

	return ipAddr.String(), nil
}

// buildBaseGenOptions creates the base generate options for Talos config generation.
func buildBaseGenOptions(controlPlaneIP string) []generate.Option {
	return []generate.Option{
		generate.WithEndpointList([]string{controlPlaneIP}),
		generate.WithAdditionalSubjectAltNames([]string{"127.0.0.1"}),
		generate.WithVersionContract(talosconfig.TalosVersion1_11),
		// Install disk is required for bare metal installations (Hetzner, etc.)
		// For Docker-in-Docker, this setting is ignored as there's no actual disk.
		// /dev/sda is the standard disk for Hetzner VPS and most cloud providers.
		generate.WithInstallDisk("/dev/sda"),
	}
}

// buildBundleOptions creates bundle options with patches applied.
func buildBundleOptions(
	clusterName string,
	controlPlaneEndpoint string,
	kubernetesVersion string,
	genOptions []generate.Option,
	clusterPatches, controlPlanePatches, workerPatches []configpatcher.Patch,
) []bundle.Option {
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

	return bundleOpts
}

// newConfigsWithEndpointAndSecrets creates Configs with an explicit endpoint IP while preserving
// an existing secrets bundle. This is used by WithEndpoint to regenerate configs with a new
// endpoint without regenerating the PKI (CA, keys, tokens), which would cause certificate mismatches.
func newConfigsWithEndpointAndSecrets(
	clusterName string,
	kubernetesVersion string,
	networkCIDR string,
	endpointIP string,
	patches []Patch,
	existingSecrets *secrets.Bundle,
) (*Configs, error) {
	// Categorize patches by scope
	clusterPatches, controlPlanePatches, workerPatches, err := categorizePatchesByScope(patches)
	if err != nil {
		return nil, err
	}

	// Resolve the control plane IP
	controlPlaneIP, err := resolveControlPlaneIP(endpointIP, networkCIDR)
	if err != nil {
		return nil, err
	}

	controlPlaneEndpoint := "https://" + net.JoinHostPort(controlPlaneIP, "6443")

	// Build generate options
	genOptions := buildBaseGenOptions(controlPlaneIP)

	// If we have existing secrets, reuse them to preserve PKI across endpoint changes
	if existingSecrets != nil {
		genOptions = append(genOptions, generate.WithSecretsBundle(existingSecrets))
	}

	// Build bundle options with patches
	bundleOpts := buildBundleOptions(
		clusterName,
		controlPlaneEndpoint,
		kubernetesVersion,
		genOptions,
		clusterPatches,
		controlPlanePatches,
		workerPatches,
	)

	// Create the bundle
	configBundle, err := bundle.NewBundle(bundleOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create config bundle: %w", err)
	}

	return &Configs{
		Name:              clusterName,
		bundle:            configBundle,
		kubernetesVersion: kubernetesVersion,
		networkCIDR:       networkCIDR,
		endpoint:          endpointIP,
		patches:           patches,
	}, nil
}

// MirrorRegistry represents a registry mirror configuration.
type MirrorRegistry struct {
	// Host is the registry host (e.g., "docker.io", "ghcr.io").
	Host string
	// Endpoints are the mirror endpoints to use (e.g., "http://docker.io:5000").
	Endpoints []string
	// Username is the optional username for registry authentication.
	// Environment variable placeholders should be resolved before passing.
	Username string
	// Password is the optional password for registry authentication.
	// Environment variable placeholders should be resolved before passing.
	Password string
}

// ApplyMirrorRegistries modifies the configs to add registry mirror configurations.
// This directly patches the underlying v1alpha1.Config structs.
// It adds both the mirror endpoints and the registry config with insecureSkipVerify: true
// to allow HTTP connections to local registry containers.
func (c *Configs) ApplyMirrorRegistries(mirrors []MirrorRegistry) error {
	if len(mirrors) == 0 || c.bundle == nil {
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

		// Add authentication if credentials are provided
		if mirror.Username != "" || mirror.Password != "" {
			addRegistryAuth(cfg, mirror)
		}
		// NOTE: We intentionally do NOT call addInsecureRegistryConfigs for HTTP endpoints.
		// containerd will reject TLS configuration for non-HTTPS registries with the error:
		// "TLS config specified for non-HTTPS registry"
		// HTTP endpoints work without any additional configuration.
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

// addRegistryAuth adds authentication configuration for a registry host.
// This configures machine.registries.config.<host>.auth with username and password.
//
//nolint:staticcheck // MachineRegistries is deprecated but still functional in Talos v1.x
func addRegistryAuth(cfg *v1alpha1.Config, mirror MirrorRegistry) {
	// Ensure the registry config exists for this host
	if cfg.MachineConfig.MachineRegistries.RegistryConfig[mirror.Host] == nil {
		cfg.MachineConfig.MachineRegistries.RegistryConfig[mirror.Host] = &v1alpha1.RegistryConfig{}
	}

	// Set the auth configuration
	cfg.MachineConfig.MachineRegistries.RegistryConfig[mirror.Host].RegistryAuth = &v1alpha1.RegistryAuthConfig{
		RegistryUsername: mirror.Username,
		RegistryPassword: mirror.Password,
	}
}
