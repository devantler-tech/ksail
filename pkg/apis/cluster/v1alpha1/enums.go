package v1alpha1

import (
	"fmt"
	"slices"
	"strings"
)

// --- Enum Interface ---

// EnumValuer is implemented by string-based enum types to provide their valid values.
// The schema generator uses this interface to automatically discover enum constraints.
type EnumValuer interface {
	// ValidValues returns all valid string values for this enum type.
	ValidValues() []string
}

// --- Distribution Types ---

// Distribution defines the distribution options for a KSail cluster.
type Distribution string

const (
	// DistributionVanilla is the vanilla Kubernetes distribution (uses Kind with Docker provider).
	DistributionVanilla Distribution = "Vanilla"
	// DistributionK3s is the K3s distribution.
	DistributionK3s Distribution = "K3s"
	// DistributionTalos is the Talos distribution.
	DistributionTalos Distribution = "Talos"
	// DistributionVCluster is the vCluster distribution (uses Vind/Docker driver).
	DistributionVCluster Distribution = "VCluster"
)

// ProvidesMetricsServerByDefault returns true if the distribution includes metrics-server by default.
// K3s includes metrics-server. VCluster inherits metrics-server from the host cluster.
// Vanilla and Talos do not.
func (d *Distribution) ProvidesMetricsServerByDefault() bool {
	switch *d {
	case DistributionK3s, DistributionVCluster:
		return true
	case DistributionVanilla, DistributionTalos:
		return false
	default:
		return false
	}
}

// ProvidesStorageByDefault returns true if the distribution includes a storage provisioner by default.
// K3s includes local-path-provisioner. VCluster inherits storage from the host cluster.
// Vanilla and Talos do not have a default storage class.
func (d *Distribution) ProvidesStorageByDefault() bool {
	switch *d {
	case DistributionK3s, DistributionVCluster:
		return true
	case DistributionVanilla, DistributionTalos:
		return false
	default:
		return false
	}
}

// ProvidesCSIByDefault returns true if the distribution × provider combination includes CSI by default.
// - K3s includes local-path-provisioner by default (regardless of provider)
// - Talos × Hetzner uses Hetzner CSI driver by default
// - VCluster inherits CSI from the host cluster
// - Vanilla and Talos × Docker do not have a default CSI.
func (d *Distribution) ProvidesCSIByDefault(provider Provider) bool {
	switch *d {
	case DistributionK3s, DistributionVCluster:
		// K3s always includes local-path-provisioner
		// VCluster inherits storage from the host cluster
		return true
	case DistributionTalos:
		// Talos × Hetzner provides Hetzner CSI by default
		return provider == ProviderHetzner
	case DistributionVanilla:
		// Vanilla (Kind) does not provide CSI by default
		return false
	default:
		return false
	}
}

// ProvidesLoadBalancerByDefault returns true if the distribution × provider combination
// includes LoadBalancer support by default.
// - K3s includes ServiceLB (Klipper-LB) by default (regardless of provider)
// - Talos × Hetzner uses hcloud-cloud-controller-manager by default
// - VCluster delegates LoadBalancer to the host cluster
// - Vanilla and Talos × Docker do not have default LoadBalancer support.
func (d *Distribution) ProvidesLoadBalancerByDefault(provider Provider) bool {
	switch *d {
	case DistributionK3s, DistributionVCluster:
		// K3s always includes ServiceLB (Klipper-LB)
		// VCluster delegates LoadBalancer to the host cluster
		return true
	case DistributionTalos:
		// Talos × Hetzner provides hcloud-cloud-controller-manager by default
		return provider == ProviderHetzner
	case DistributionVanilla:
		// Vanilla (Kind) does not provide LoadBalancer by default
		return false
	default:
		return false
	}
}

// Set for Distribution (pflag.Value interface).
func (d *Distribution) Set(value string) error {
	for _, dist := range ValidDistributions() {
		if strings.EqualFold(value, string(dist)) {
			*d = dist

			return nil
		}
	}

	return fmt.Errorf(
		"%w: %s (valid options: %s, %s, %s, %s)",
		ErrInvalidDistribution,
		value,
		DistributionVanilla,
		DistributionK3s,
		DistributionTalos,
		DistributionVCluster,
	)
}

// IsValid checks if the distribution value is supported.
func (d *Distribution) IsValid() bool {
	return slices.Contains(ValidDistributions(), *d)
}

// String returns the string representation of the Distribution.
func (d *Distribution) String() string {
	return string(*d)
}

// Type returns the type of the Distribution.
func (d *Distribution) Type() string {
	return "Distribution"
}

// Default returns the default value for Distribution (Vanilla).
func (d *Distribution) Default() any {
	return DistributionVanilla
}

// ValidValues returns all valid Distribution values as strings.
func (d *Distribution) ValidValues() []string {
	return []string{
		string(DistributionVanilla),
		string(DistributionK3s),
		string(DistributionTalos),
		string(DistributionVCluster),
	}
}

// ContextName returns the kubeconfig context name for a given cluster name.
// Each distribution has its own context naming convention:
//   - Vanilla: kind-<name>
//   - K3s: k3d-<name>
//   - Talos: admin@<name>
//
// Returns empty string if name is empty.
func (d *Distribution) ContextName(clusterName string) string {
	if clusterName == "" {
		return ""
	}

	switch *d {
	case DistributionVanilla:
		return "kind-" + clusterName
	case DistributionK3s:
		return "k3d-" + clusterName
	case DistributionTalos:
		return "admin@" + clusterName
	case DistributionVCluster:
		return "vcluster-docker_" + clusterName
	default:
		return ""
	}
}

// DefaultClusterName returns the default cluster name for a distribution.
// Each distribution has its own default naming convention:
//   - Vanilla: "kind"
//   - K3s: "k3d-default"
//   - Talos: "talos-default"
//
// Returns "kind" for unknown distributions.
func (d *Distribution) DefaultClusterName() string {
	switch *d {
	case DistributionVanilla:
		return "kind"
	case DistributionK3s:
		return "k3d-default"
	case DistributionTalos:
		return "talos-default"
	case DistributionVCluster:
		return "vcluster-default"
	default:
		return "kind"
	}
}

// --- CNI Types ---

// CNI defines the CNI options for a KSail cluster.
type CNI string

const (
	// CNIDefault is the default CNI.
	CNIDefault CNI = "Default"
	// CNICilium is the Cilium CNI.
	CNICilium CNI = "Cilium"
	// CNICalico is the Calico CNI.
	CNICalico CNI = "Calico"
)

// Set for CNI (pflag.Value interface).
func (c *CNI) Set(value string) error {
	for _, cni := range ValidCNIs() {
		if strings.EqualFold(value, string(cni)) {
			*c = cni

			return nil
		}
	}

	return fmt.Errorf("%w: %s (valid options: %s, %s, %s)",
		ErrInvalidCNI, value, CNIDefault, CNICilium, CNICalico)
}

// String returns the string representation of the CNI.
func (c *CNI) String() string {
	return string(*c)
}

// Type returns the type of the CNI.
func (c *CNI) Type() string {
	return "CNI"
}

// Default returns the default value for CNI (Default).
func (c *CNI) Default() any {
	return CNIDefault
}

// ValidValues returns all valid CNI values as strings.
func (c *CNI) ValidValues() []string {
	return []string{string(CNIDefault), string(CNICilium), string(CNICalico)}
}

// --- CSI Types ---

// CSI defines the CSI options for a KSail cluster.
type CSI string

const (
	// CSIDefault relies on the distribution's default behavior for CSI.
	CSIDefault CSI = "Default"
	// CSIEnabled ensures a CSI driver is installed (local-path-provisioner or Hetzner CSI).
	CSIEnabled CSI = "Enabled"
	// CSIDisabled ensures no CSI driver is installed.
	CSIDisabled CSI = "Disabled"
)

// Set for CSI (pflag.Value interface).
func (c *CSI) Set(value string) error {
	for _, csi := range ValidCSIs() {
		if strings.EqualFold(value, string(csi)) {
			*c = csi

			return nil
		}
	}

	return fmt.Errorf("%w: %s (valid options: %s, %s, %s)",
		ErrInvalidCSI, value, CSIDefault, CSIEnabled, CSIDisabled)
}

// String returns the string representation of the CSI.
func (c *CSI) String() string {
	return string(*c)
}

// Type returns the type of the CSI.
func (c *CSI) Type() string {
	return "CSI"
}

// Default returns the default value for CSI (Default).
func (c *CSI) Default() any {
	return CSIDefault
}

// ValidValues returns all valid CSI values as strings.
func (c *CSI) ValidValues() []string {
	return []string{string(CSIDefault), string(CSIEnabled), string(CSIDisabled)}
}

// EffectiveValue resolves Default to its concrete meaning for the given
// distribution × provider combination. Enabled and Disabled pass through
// unchanged. For distributions that bundle a CSI driver (e.g. K3s),
// Default resolves to Enabled; otherwise it resolves to Disabled.
func (c *CSI) EffectiveValue(distribution Distribution, provider Provider) CSI {
	if *c != CSIDefault {
		return *c
	}

	if distribution.ProvidesCSIByDefault(provider) {
		return CSIEnabled
	}

	return CSIDisabled
}

// --- Metrics Server Types ---

// MetricsServer defines the Metrics Server options for a KSail cluster.
type MetricsServer string

const (
	// MetricsServerDefault relies on the distribution's default behavior for metrics server.
	MetricsServerDefault MetricsServer = "Default"
	// MetricsServerEnabled ensures Metrics Server is installed.
	MetricsServerEnabled MetricsServer = "Enabled"
	// MetricsServerDisabled ensures Metrics Server is not installed.
	MetricsServerDisabled MetricsServer = "Disabled"
)

// Set for MetricsServer (pflag.Value interface).
func (m *MetricsServer) Set(value string) error {
	for _, ms := range ValidMetricsServers() {
		if strings.EqualFold(value, string(ms)) {
			*m = ms

			return nil
		}
	}

	return fmt.Errorf(
		"%w: %s (valid options: %s, %s, %s)",
		ErrInvalidMetricsServer,
		value,
		MetricsServerDefault,
		MetricsServerEnabled,
		MetricsServerDisabled,
	)
}

// String returns the string representation of the MetricsServer.
func (m *MetricsServer) String() string {
	return string(*m)
}

// Type returns the type of the MetricsServer.
func (m *MetricsServer) Type() string {
	return "MetricsServer"
}

// Default returns the default value for MetricsServer (Default, which defers to the distribution).
func (m *MetricsServer) Default() any {
	return MetricsServerDefault
}

// ValidValues returns all valid MetricsServer values as strings.
func (m *MetricsServer) ValidValues() []string {
	return []string{
		string(MetricsServerDefault),
		string(MetricsServerEnabled),
		string(MetricsServerDisabled),
	}
}

// EffectiveValue resolves Default to its concrete meaning for the given
// distribution. Enabled and Disabled pass through unchanged. For
// distributions that bundle metrics-server (e.g. K3s), Default resolves
// to Enabled; otherwise it resolves to Disabled.
func (m *MetricsServer) EffectiveValue(distribution Distribution) MetricsServer {
	if *m != MetricsServerDefault {
		return *m
	}

	if distribution.ProvidesMetricsServerByDefault() {
		return MetricsServerEnabled
	}

	return MetricsServerDisabled
}

// --- Load Balancer Types ---

// LoadBalancer defines the LoadBalancer options for a KSail cluster.
type LoadBalancer string

const (
	// LoadBalancerDefault relies on the distribution × provider default behavior for LoadBalancer support.
	LoadBalancerDefault LoadBalancer = "Default"
	// LoadBalancerEnabled ensures LoadBalancer support is enabled.
	LoadBalancerEnabled LoadBalancer = "Enabled"
	// LoadBalancerDisabled ensures LoadBalancer support is disabled.
	LoadBalancerDisabled LoadBalancer = "Disabled"
)

// Set for LoadBalancer (pflag.Value interface).
func (l *LoadBalancer) Set(value string) error {
	for _, lb := range ValidLoadBalancers() {
		if strings.EqualFold(value, string(lb)) {
			*l = lb

			return nil
		}
	}

	return fmt.Errorf(
		"%w: %s (valid options: %s, %s, %s)",
		ErrInvalidLoadBalancer,
		value,
		LoadBalancerDefault,
		LoadBalancerEnabled,
		LoadBalancerDisabled,
	)
}

// String returns the string representation of the LoadBalancer.
func (l *LoadBalancer) String() string {
	return string(*l)
}

// Type returns the type of the LoadBalancer.
func (l *LoadBalancer) Type() string {
	return "LoadBalancer"
}

// Default returns the default value for LoadBalancer (Default, which defers to the distribution × provider).
func (l *LoadBalancer) Default() any {
	return LoadBalancerDefault
}

// ValidValues returns all valid LoadBalancer values as strings.
func (l *LoadBalancer) ValidValues() []string {
	return []string{
		string(LoadBalancerDefault),
		string(LoadBalancerEnabled),
		string(LoadBalancerDisabled),
	}
}

// EffectiveValue resolves Default to its concrete meaning for the given
// distribution × provider combination. Enabled and Disabled pass through
// unchanged. For distributions that bundle a load balancer (e.g. K3s),
// Default resolves to Enabled; otherwise it resolves to Disabled.
func (l *LoadBalancer) EffectiveValue(
	distribution Distribution, provider Provider,
) LoadBalancer {
	if *l != LoadBalancerDefault {
		return *l
	}

	if distribution.ProvidesLoadBalancerByDefault(provider) {
		return LoadBalancerEnabled
	}

	return LoadBalancerDisabled
}

// --- Cert-Manager Types ---

// CertManager defines the cert-manager options for a KSail cluster.
type CertManager string

const (
	// CertManagerEnabled ensures cert-manager is installed.
	CertManagerEnabled CertManager = "Enabled"
	// CertManagerDisabled ensures cert-manager is not installed.
	CertManagerDisabled CertManager = "Disabled"
)

// Set for CertManager (pflag.Value interface).
func (c *CertManager) Set(value string) error {
	for _, cm := range ValidCertManagers() {
		if strings.EqualFold(value, string(cm)) {
			*c = cm

			return nil
		}
	}

	return fmt.Errorf(
		"%w: %s (valid options: %s, %s)",
		ErrInvalidCertManager,
		value,
		CertManagerEnabled,
		CertManagerDisabled,
	)
}

// String returns the string representation of the CertManager.
func (c *CertManager) String() string {
	return string(*c)
}

// Type returns the type of the CertManager.
func (c *CertManager) Type() string {
	return "CertManager"
}

// Default returns the default value for CertManager (Disabled).
func (c *CertManager) Default() any {
	return CertManagerDisabled
}

// ValidValues returns all valid CertManager values as strings.
func (c *CertManager) ValidValues() []string {
	return []string{string(CertManagerEnabled), string(CertManagerDisabled)}
}

// --- Policy Engine Types ---

// PolicyEngine defines the policy engine options for a KSail cluster.
type PolicyEngine string

const (
	// PolicyEngineNone is the default and disables policy engine installation.
	PolicyEngineNone PolicyEngine = "None"
	// PolicyEngineKyverno installs Kyverno.
	PolicyEngineKyverno PolicyEngine = "Kyverno"
	// PolicyEngineGatekeeper installs OPA Gatekeeper.
	PolicyEngineGatekeeper PolicyEngine = "Gatekeeper"
)

// Set for PolicyEngine (pflag.Value interface).
func (p *PolicyEngine) Set(value string) error {
	for _, pe := range ValidPolicyEngines() {
		if strings.EqualFold(value, string(pe)) {
			*p = pe

			return nil
		}
	}

	return fmt.Errorf(
		"%w: %s (valid options: %s, %s, %s)",
		ErrInvalidPolicyEngine,
		value,
		PolicyEngineNone,
		PolicyEngineKyverno,
		PolicyEngineGatekeeper,
	)
}

// String returns the string representation of the PolicyEngine.
func (p *PolicyEngine) String() string {
	return string(*p)
}

// Type returns the type of the PolicyEngine.
func (p *PolicyEngine) Type() string {
	return "PolicyEngine"
}

// Default returns the default value for PolicyEngine (None).
func (p *PolicyEngine) Default() any {
	return PolicyEngineNone
}

// ValidValues returns all valid PolicyEngine values as strings.
func (p *PolicyEngine) ValidValues() []string {
	return []string{
		string(PolicyEngineNone),
		string(PolicyEngineKyverno),
		string(PolicyEngineGatekeeper),
	}
}

// --- GitOps Engine Types ---

// GitOpsEngine defines the GitOps Engine options for a KSail cluster.
type GitOpsEngine string

const (
	// GitOpsEngineNone is the default and disables managed GitOps integration.
	// It means "no GitOps engine" is configured for the cluster.
	GitOpsEngineNone GitOpsEngine = "None"
	// GitOpsEngineFlux installs and manages Flux controllers.
	GitOpsEngineFlux GitOpsEngine = "Flux"
	// GitOpsEngineArgoCD installs and manages Argo CD.
	GitOpsEngineArgoCD GitOpsEngine = "ArgoCD"
)

// Set for GitOpsEngine (pflag.Value interface).
func (g *GitOpsEngine) Set(value string) error {
	for _, tool := range ValidGitOpsEngines() {
		if strings.EqualFold(value, string(tool)) {
			*g = tool

			return nil
		}
	}

	return fmt.Errorf(
		"%w: %s (valid options: %s, %s, %s)",
		ErrInvalidGitOpsEngine,
		value,
		GitOpsEngineNone,
		GitOpsEngineFlux,
		GitOpsEngineArgoCD,
	)
}

// String returns the string representation of the GitOpsEngine.
func (g *GitOpsEngine) String() string {
	return string(*g)
}

// Type returns the type of the GitOpsEngine.
func (g *GitOpsEngine) Type() string {
	return "GitOpsEngine"
}

// Default returns the default value for GitOpsEngine (None).
func (g *GitOpsEngine) Default() any {
	return GitOpsEngineNone
}

// ValidValues returns all valid GitOpsEngine values as strings.
func (g *GitOpsEngine) ValidValues() []string {
	return []string{string(GitOpsEngineNone), string(GitOpsEngineFlux), string(GitOpsEngineArgoCD)}
}

// --- Placement Group Strategy Types ---

// PlacementGroupStrategy defines the placement group strategy for Hetzner Cloud servers.
type PlacementGroupStrategy string

const (
	// PlacementGroupStrategyNone disables placement group usage.
	// Servers can be placed on any available host, which may result in
	// multiple servers on the same physical host.
	PlacementGroupStrategyNone PlacementGroupStrategy = "None"
	// PlacementGroupStrategySpread ensures servers are distributed across
	// different physical hosts for high availability. Note: Hetzner limits
	// spread groups to 10 servers per datacenter.
	PlacementGroupStrategySpread PlacementGroupStrategy = "Spread"
)

// Set for PlacementGroupStrategy (pflag.Value interface).
func (p *PlacementGroupStrategy) Set(value string) error {
	for _, strategy := range ValidPlacementGroupStrategies() {
		if strings.EqualFold(value, string(strategy)) {
			*p = strategy

			return nil
		}
	}

	return fmt.Errorf(
		"%w: %s (valid options: %s, %s)",
		ErrInvalidPlacementGroupStrategy,
		value,
		PlacementGroupStrategyNone,
		PlacementGroupStrategySpread,
	)
}

// String returns the string representation of the PlacementGroupStrategy.
func (p *PlacementGroupStrategy) String() string {
	return string(*p)
}

// Type returns the type of the PlacementGroupStrategy.
func (p *PlacementGroupStrategy) Type() string {
	return "PlacementGroupStrategy"
}

// Default returns the default value for PlacementGroupStrategy (Spread).
func (p *PlacementGroupStrategy) Default() any {
	return PlacementGroupStrategySpread
}

// ValidValues returns all valid PlacementGroupStrategy values as strings.
func (p *PlacementGroupStrategy) ValidValues() []string {
	return []string{string(PlacementGroupStrategyNone), string(PlacementGroupStrategySpread)}
}

// --- Provider Types ---

// Provider defines the infrastructure provider backend for running clusters.
// This is a unified type used across distributions that support multiple providers.
type Provider string

const (
	// ProviderDocker runs cluster nodes as Docker containers.
	ProviderDocker Provider = "Docker"
	// ProviderHetzner runs cluster nodes as Hetzner Cloud servers.
	ProviderHetzner Provider = "Hetzner"
)

// Set for Provider (pflag.Value interface).
func (p *Provider) Set(value string) error {
	for _, prov := range ValidProviders() {
		if strings.EqualFold(value, string(prov)) {
			*p = prov

			return nil
		}
	}

	return fmt.Errorf(
		"%w: %s (valid options: %s, %s)",
		ErrInvalidProvider,
		value,
		ProviderDocker,
		ProviderHetzner,
	)
}

// String returns the string representation of the Provider.
func (p *Provider) String() string {
	return string(*p)
}

// Type returns the type of the Provider.
func (p *Provider) Type() string {
	return "Provider"
}

// Default returns the default value for Provider (Docker).
func (p *Provider) Default() any {
	return ProviderDocker
}

// ValidValues returns all valid Provider values as strings.
func (p *Provider) ValidValues() []string {
	return []string{string(ProviderDocker), string(ProviderHetzner)}
}

// supportedProviders returns the valid providers for a given distribution.
func supportedProviders(distribution Distribution) []Provider {
	switch distribution {
	case DistributionVanilla, DistributionK3s, DistributionVCluster:
		return []Provider{ProviderDocker}
	case DistributionTalos:
		return []Provider{ProviderDocker, ProviderHetzner}
	default:
		return nil
	}
}

// ValidateForDistribution validates that the provider is valid for the given distribution.
// Returns nil if the combination is valid, or an error describing the invalid combination.
func (p *Provider) ValidateForDistribution(distribution Distribution) error {
	supported := supportedProviders(distribution)
	if supported == nil {
		return fmt.Errorf("%w: %s", ErrInvalidDistribution, distribution)
	}

	// Empty provider defaults to Docker which is always supported
	if *p == "" || slices.Contains(supported, *p) {
		return nil
	}

	return fmt.Errorf(
		"%w: distribution %s does not support provider %s",
		ErrInvalidDistributionProviderCombination,
		distribution,
		*p,
	)
}
