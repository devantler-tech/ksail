package v1alpha1

import (
	"fmt"
	"regexp"
	"strings"
)

// clusterNameRegex matches DNS-1123 subdomain names: lowercase alphanumeric with optional hyphens.
// Must start with a letter, end with alphanumeric, and be at most 63 characters.
// See: https://kubernetes.io/docs/concepts/overview/working-with-objects/names/#dns-subdomain-names
var clusterNameRegex = regexp.MustCompile(`^[a-z][a-z0-9-]*[a-z0-9]$|^[a-z]$`)

// ClusterNameMaxLength is the maximum length for a cluster name.
const ClusterNameMaxLength = 63

// ValidateClusterName validates that a cluster name is DNS-1123 compliant.
// Cluster names are used in Docker container names, Kubernetes contexts, and YAML fields,
// which require DNS-1123 subdomain names (lowercase alphanumeric and dashes only).
//
// Returns nil if the name is valid, or an error describing the validation failure.
func ValidateClusterName(name string) error {
	if name == "" {
		return nil // Empty names are allowed (means use default)
	}

	if len(name) > ClusterNameMaxLength {
		return fmt.Errorf(
			"%w: %q exceeds max %d characters (got %d)",
			ErrClusterNameTooLong, name, ClusterNameMaxLength, len(name),
		)
	}

	if !clusterNameRegex.MatchString(name) {
		return fmt.Errorf(
			"%w: %q must be DNS-1123 compliant "+
				"(lowercase letters, numbers, and hyphens; must start with a letter; "+
				"must not end with a hyphen)",
			ErrClusterNameInvalid, name,
		)
	}

	return nil
}

// ValidDistributions returns supported distribution values.
func ValidDistributions() []Distribution {
	return []Distribution{
		DistributionVanilla,
		DistributionK3s,
		DistributionTalos,
		DistributionVCluster,
		DistributionKWOK,
		DistributionEKS,
	}
}

// ValidGitOpsEngines enumerates supported GitOps engine values.
func ValidGitOpsEngines() []GitOpsEngine {
	return []GitOpsEngine{
		GitOpsEngineNone,
		GitOpsEngineFlux,
		GitOpsEngineArgoCD,
	}
}

// ValidCNIs returns supported CNI values.
func ValidCNIs() []CNI {
	return []CNI{CNIDefault, CNICilium, CNICalico}
}

// ValidCSIs returns supported CSI values.
func ValidCSIs() []CSI {
	return []CSI{CSIDefault, CSIEnabled, CSIDisabled}
}

// ValidCDIs returns supported CDI values.
func ValidCDIs() []CDI {
	return []CDI{CDIDefault, CDIEnabled, CDIDisabled}
}

// ValidMetricsServers returns supported metrics server values.
func ValidMetricsServers() []MetricsServer {
	return []MetricsServer{
		MetricsServerDefault,
		MetricsServerEnabled,
		MetricsServerDisabled,
	}
}

// ValidLoadBalancers returns supported load balancer values.
func ValidLoadBalancers() []LoadBalancer {
	return []LoadBalancer{
		LoadBalancerDefault,
		LoadBalancerEnabled,
		LoadBalancerDisabled,
	}
}

// ValidCertManagers returns supported cert-manager values.
func ValidCertManagers() []CertManager {
	return []CertManager{
		CertManagerEnabled,
		CertManagerDisabled,
	}
}

// ValidImageVerifications returns supported image verification values.
func ValidImageVerifications() []ImageVerification {
	return []ImageVerification{
		ImageVerificationEnabled,
		ImageVerificationDisabled,
	}
}

// ValidPolicyEngines returns supported policy engine values.
func ValidPolicyEngines() []PolicyEngine {
	return []PolicyEngine{
		PolicyEngineNone,
		PolicyEngineKyverno,
		PolicyEngineGatekeeper,
	}
}

// ValidProviders returns supported provider values.
func ValidProviders() []Provider {
	return []Provider{ProviderDocker, ProviderHetzner, ProviderOmni, ProviderAWS}
}

// ValidPlacementGroupStrategies returns supported placement group strategy values.
func ValidPlacementGroupStrategies() []PlacementGroupStrategy {
	return []PlacementGroupStrategy{PlacementGroupStrategyNone, PlacementGroupStrategySpread}
}

// ValidNodeAutoscalings returns supported node autoscaling values.
func ValidNodeAutoscalings() []NodeAutoscaling {
	return []NodeAutoscaling{NodeAutoscalingEnabled, NodeAutoscalingDisabled}
}

// ValidNodeAutoscalerEnableds returns supported NodeAutoscalerEnabled values.
func ValidNodeAutoscalerEnableds() []NodeAutoscalerEnabled {
	return []NodeAutoscalerEnabled{NodeAutoscalerEnabledEnabled, NodeAutoscalerEnabledDisabled}
}

// ValidAutoscalerExpanders returns supported AutoscalerExpander values.
func ValidAutoscalerExpanders() []AutoscalerExpander {
	return []AutoscalerExpander{
		AutoscalerExpanderPrice,
		AutoscalerExpanderLeastWaste,
		AutoscalerExpanderLeastNodes,
		AutoscalerExpanderRandom,
	}
}

// ValidPodAutoscalerHorizontals returns supported PodAutoscalerHorizontal values.
func ValidPodAutoscalerHorizontals() []PodAutoscalerHorizontal {
	return []PodAutoscalerHorizontal{
		PodAutoscalerHorizontalEnabled,
		PodAutoscalerHorizontalDisabled,
	}
}

// ValidPodAutoscalerVerticals returns supported PodAutoscalerVertical values.
func ValidPodAutoscalerVerticals() []PodAutoscalerVertical {
	return []PodAutoscalerVertical{PodAutoscalerVerticalEnabled, PodAutoscalerVerticalDisabled}
}

// ValidateMirrorRegistriesForProvider validates that mirror registries are compatible with the provider.
// Cloud providers (like Hetzner) cannot access local Docker containers running as mirror registries.
// For cloud providers, mirror registries must point to external, internet-accessible registries.
//
// Parameters:
//   - provider: The infrastructure provider being used
//   - mirrorRegistries: List of mirror registry specifications
//
// Returns nil if valid, or an error if local mirrors are configured for a cloud provider.
func ValidateMirrorRegistriesForProvider(provider Provider, mirrorRegistries []string) error {
	if len(mirrorRegistries) == 0 {
		return nil
	}

	// Cloud providers cannot access local Docker containers as mirrors
	if provider == ProviderHetzner || provider == ProviderOmni || provider == ProviderAWS {
		for _, spec := range mirrorRegistries {
			if isLocalMirrorSpec(spec) {
				return fmt.Errorf(
					"%w: %q references a local endpoint; "+
						"cloud-based clusters cannot access local Docker containers; "+
						"use external registry URLs instead (e.g., docker.io=https://registry-1.docker.io)",
					ErrMirrorRegistryNotSupported,
					spec,
				)
			}
		}
	}

	return nil
}

// isLocalMirrorSpec checks if a mirror specification references a local endpoint.
// Local endpoints include localhost, 127.0.0.1, 0.0.0.0, and host.docker.internal.
func isLocalMirrorSpec(spec string) bool {
	// Normalize to lowercase for comparison
	lower := strings.ToLower(spec)

	// Check for local patterns in the spec
	// These patterns indicate local Docker container references that won't work on cloud providers
	localPatterns := []string{
		"localhost",
		"127.0.0.1",
		"0.0.0.0",
		"host.docker.internal",
		"[::1]", // IPv6 localhost
	}

	for _, pattern := range localPatterns {
		if strings.Contains(lower, pattern) {
			return true
		}
	}

	return false
}

// ValidateLocalRegistryForProvider validates that local registry configuration is compatible with the provider.
// Cloud providers (like Hetzner) cannot use local Docker registries and must use external registries.
//
// Parameters:
//   - provider: The infrastructure provider being used
//   - registry: The local registry configuration
//
// Returns nil if valid, or an error if local registry is enabled without external host for cloud provider.
func ValidateLocalRegistryForProvider(provider Provider, registry LocalRegistry) error {
	if !registry.Enabled() {
		return nil
	}

	// Cloud providers require external registries with proper host configuration
	if (provider == ProviderHetzner || provider == ProviderOmni || provider == ProviderAWS) &&
		!registry.IsExternal() {
		return ErrLocalRegistryNotSupported
	}

	return nil
}

// poolNameRegex matches DNS-1123 label names: lowercase alphanumeric with optional hyphens.
// Must start and end with alphanumeric, and be at most 63 characters.
var poolNameRegex = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*[a-z0-9]$|^[a-z0-9]$`)

// PoolNameMaxLength is the maximum length for a node pool name (DNS-1123 label limit).
const PoolNameMaxLength = 63

// validateNodePools checks each NodePool for name validity, min ≤ max, and uniqueness.
func validateNodePools(pools []NodePool) error {
	seen := make(map[string]struct{}, len(pools))

	for idx, pool := range pools {
		if len(pool.Name) > PoolNameMaxLength {
			return fmt.Errorf(
				"%w: pool[%d] %q exceeds the 63-character DNS-1123 label limit",
				ErrInvalidPoolName, idx, pool.Name,
			)
		}

		if !poolNameRegex.MatchString(pool.Name) {
			return fmt.Errorf(
				"%w: pool[%d] %q must be a DNS-1123 label "+
					"(lowercase letters, numbers, and hyphens; must start and end with alphanumeric; "+
					"must not end with a hyphen)",
				ErrInvalidPoolName, idx, pool.Name,
			)
		}

		if pool.ServerType == "" {
			return fmt.Errorf("%w: pool[%d] %q", ErrPoolServerTypeEmpty, idx, pool.Name)
		}

		if pool.Location == "" {
			return fmt.Errorf("%w: pool[%d] %q", ErrPoolLocationEmpty, idx, pool.Name)
		}

		if pool.Min < 0 {
			return fmt.Errorf(
				"%w: pool %q has min=%d",
				ErrPoolNegativeMin, pool.Name, pool.Min,
			)
		}

		if pool.Max < 0 {
			return fmt.Errorf(
				"%w: pool %q has max=%d",
				ErrPoolNegativeMax, pool.Name, pool.Max,
			)
		}

		if pool.Min > pool.Max {
			return fmt.Errorf(
				"%w: pool %q has min=%d > max=%d",
				ErrPoolMinExceedsMax, pool.Name, pool.Min, pool.Max,
			)
		}

		if _, exists := seen[pool.Name]; exists {
			return fmt.Errorf("%w: %q", ErrDuplicatePoolName, pool.Name)
		}

		seen[pool.Name] = struct{}{}
	}

	return nil
}

// ValidateAutoscalerConfig validates the autoscaler configuration within a ClusterSpec.
// It checks pool-level constraints (name validity, min ≤ max, uniqueness) and, when
// targeting Hetzner with node autoscaling enabled, enforces that the total server
// capacity does not exceed the configured ServerLimit.
//
// Parameters:
//   - cluster: The ClusterSpec containing autoscaler and node-count configuration.
//   - provider: The ProviderSpec containing Hetzner-specific options (ServerLimit).
//
// Returns the first validation error encountered, or nil if the configuration is valid.
func ValidateAutoscalerConfig(cluster *ClusterSpec, provider *ProviderSpec) error {
	if cluster == nil {
		return nil
	}

	autoscaler := &cluster.Autoscaler.Node

	err := validateNodePools(autoscaler.Pools)
	if err != nil {
		return err
	}

	// Capacity guard: only applies when Hetzner provider and node autoscaler is enabled.
	// The deprecated cluster.NodeAutoscaling field is also checked for backward compatibility
	// during the deprecation window (before migration fully replaces it).
	autoscalingEnabled := autoscaler.Enabled == NodeAutoscalerEnabledEnabled ||
		cluster.NodeAutoscaling == NodeAutoscalingEnabled
	if provider == nil ||
		cluster.Provider != ProviderHetzner ||
		!autoscalingEnabled {
		return nil
	}

	if len(autoscaler.Pools) == 0 {
		return fmt.Errorf("%w: provider is %q", ErrAutoscalerEnabledNoPools, ProviderHetzner)
	}

	// serverLimit == 0 means "use default"; 0 is not an expressible explicit limit.
	serverLimit := provider.Hetzner.ServerLimit
	if serverLimit == 0 {
		serverLimit = DefaultHetznerServerLimit
	}

	return validateServerCapacity(cluster, autoscaler, serverLimit)
}

// validateServerCapacity checks that controlPlanes + workers + effective pool capacity
// does not exceed serverLimit. The effective pool capacity is sum(pool.Max) capped by
// MaxNodesTotal when MaxNodesTotal > 0, because the autoscaler will not exceed that bound.
func validateServerCapacity(
	cluster *ClusterSpec,
	autoscaler *NodeAutoscalerConfig,
	serverLimit int32,
) error {
	var poolCapacity int32
	for _, pool := range autoscaler.Pools {
		poolCapacity += pool.Max
	}

	// When MaxNodesTotal is set and lower than the raw pool capacity, the autoscaler
	// itself will never exceed MaxNodesTotal; use that as the effective capacity.
	effectiveCapacity := poolCapacity
	if autoscaler.MaxNodesTotal > 0 && autoscaler.MaxNodesTotal < poolCapacity {
		effectiveCapacity = autoscaler.MaxNodesTotal
	}

	total := cluster.ControlPlanes + cluster.Workers + effectiveCapacity
	if total > serverLimit {
		return fmt.Errorf(
			"%w: controlPlanes(%d) + workers(%d) + poolCapacity(%d) = %d exceeds serverLimit(%d)",
			ErrAutoscalerExceedsServerLimit,
			cluster.ControlPlanes,
			cluster.Workers,
			effectiveCapacity,
			total,
			serverLimit,
		)
	}

	return nil
}
