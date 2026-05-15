package v1alpha1

import (
	"fmt"
	"net"
	"regexp"
	"slices"
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
	return []Provider{ProviderDocker, ProviderHetzner, ProviderOmni, ProviderAWS, ProviderKubernetes}
}

// ValidPlacementGroupStrategies returns supported placement group strategy values.
func ValidPlacementGroupStrategies() []PlacementGroupStrategy {
	return []PlacementGroupStrategy{PlacementGroupStrategyNone, PlacementGroupStrategySpread}
}

// ValidNodeAutoscalings returns supported node autoscaling values.
func ValidNodeAutoscalings() []NodeAutoscaling {
	return []NodeAutoscaling{NodeAutoscalingEnabled, NodeAutoscalingDisabled}
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

	// Cloud and Kubernetes providers require external registries with proper host configuration
	if provider == ProviderHetzner ||
		provider == ProviderOmni ||
		provider == ProviderAWS ||
		provider == ProviderKubernetes {
		if !registry.IsExternal() {
			return ErrLocalRegistryNotSupported
		}
	}

	return nil
}

// poolNameRegex matches DNS-1123 label names: lowercase alphanumeric with optional hyphens.
// Must start and end with a lowercase alphanumeric character, and be at most 63 characters.
var poolNameRegex = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?$`)

// PoolNameMaxLength is the maximum length for a pool name (DNS-1123 label limit).
const PoolNameMaxLength = 63

// validateAutoscalerEnumFields validates enum-typed fields on the autoscaler configuration.
// It returns an error if any field carries a value that is not in its valid set.
func validateAutoscalerEnumFields(
	autoscaler *NodeAutoscalerConfig,
	pod *PodAutoscalerConfig,
) error {
	if autoscaler.Expander != "" &&
		!slices.Contains(ValidAutoscalerExpanders(), autoscaler.Expander) {
		return fmt.Errorf("%w: %q", ErrInvalidAutoscalerExpander, autoscaler.Expander)
	}

	if pod.Horizontal != "" && !slices.Contains(ValidPodAutoscalerHorizontals(), pod.Horizontal) {
		return fmt.Errorf("%w: %q", ErrInvalidPodAutoscalerHorizontal, pod.Horizontal)
	}

	if pod.Vertical != "" && !slices.Contains(ValidPodAutoscalerVerticals(), pod.Vertical) {
		return fmt.Errorf("%w: %q", ErrInvalidPodAutoscalerVertical, pod.Vertical)
	}

	return nil
}

// validateExpanderForProvider checks that the chosen autoscaler expander
// strategy is supported by the cluster's infrastructure provider. The Hetzner
// cloud provider in the upstream cluster-autoscaler does not implement the
// pricing API, so the Price expander causes a fatal crash on startup.
func validateExpanderForProvider(
	provider Provider,
	autoscaler *NodeAutoscalerConfig,
) error {
	if !autoscaler.Enabled || autoscaler.Expander == "" {
		return nil
	}

	if provider == ProviderHetzner && autoscaler.Expander == AutoscalerExpanderPrice {
		return fmt.Errorf(
			"%w: %q is not supported on %s (Hetzner does not implement the pricing API); "+
				"use %s or %s instead",
			ErrExpanderNotSupportedForProvider,
			autoscaler.Expander,
			ProviderHetzner,
			AutoscalerExpanderLeastWaste,
			AutoscalerExpanderRandom,
		)
	}

	return nil
}

// validateNodePools checks each NodePool for name validity, min ≤ max, and uniqueness.
func validateNodePools(pools []NodePool) error {
	seen := make(map[string]struct{}, len(pools))

	for idx, pool := range pools {
		if len(pool.Name) > PoolNameMaxLength {
			return fmt.Errorf(
				"%w: pool[%d] %q exceeds max %d characters (got %d)",
				ErrInvalidPoolName, idx, pool.Name, PoolNameMaxLength, len(pool.Name),
			)
		}

		if !poolNameRegex.MatchString(pool.Name) {
			return fmt.Errorf(
				"%w: pool[%d] %q must be a DNS-1123 label "+
					"(lowercase letters, numbers, and hyphens; "+
					"must start and end with a lowercase alphanumeric character)",
				ErrInvalidPoolName, idx, pool.Name,
			)
		}

		if pool.ServerType == "" {
			return fmt.Errorf("%w: pool[%d] %q", ErrPoolServerTypeEmpty, idx, pool.Name)
		}

		if pool.Location == "" {
			return fmt.Errorf("%w: pool[%d] %q", ErrPoolLocationEmpty, idx, pool.Name)
		}

		if pool.Min < 0 || pool.Max < 0 {
			return fmt.Errorf(
				"%w: pool %q has negative min=%d or max=%d",
				ErrInvalidPoolCapacity, pool.Name, pool.Min, pool.Max,
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
func ValidateAutoscalerConfig(
	cluster *ClusterSpec,
	provider *ProviderSpec,
) error {
	if cluster == nil {
		return nil
	}

	autoscaler := &cluster.Autoscaler.Node

	enumErr := validateAutoscalerEnumFields(autoscaler, &cluster.Autoscaler.Pod)
	if enumErr != nil {
		return enumErr
	}

	expanderErr := validateExpanderForProvider(cluster.Provider, autoscaler)
	if expanderErr != nil {
		return expanderErr
	}

	if autoscaler.Enabled && len(autoscaler.Pools) == 0 {
		return ErrAutoscalerEnabledNoPools
	}

	err := validateNodePools(autoscaler.Pools)
	if err != nil {
		return err
	}

	return validateHetznerCapacity(cluster, provider, autoscaler)
}

// validateHetznerCapacity checks that controlPlanes+workers+effectivePoolCapacity ≤ serverLimit
// when provider is Hetzner and node autoscaling is enabled.
func validateHetznerCapacity(
	cluster *ClusterSpec,
	provider *ProviderSpec,
	autoscaler *NodeAutoscalerConfig,
) error {
	if provider == nil ||
		cluster.Provider != ProviderHetzner ||
		!autoscaler.Enabled {
		return nil
	}

	if autoscaler.MaxNodesTotal < 0 {
		return fmt.Errorf("%w: got %d", ErrInvalidMaxNodesTotal, autoscaler.MaxNodesTotal)
	}

	serverLimit, err := resolveServerLimit(provider.Hetzner.ServerLimit)
	if err != nil {
		return err
	}

	var poolCapacity int32
	for _, pool := range autoscaler.Pools {
		poolCapacity += pool.Max
	}

	effectivePoolCapacity := poolCapacity
	if autoscaler.MaxNodesTotal > 0 && autoscaler.MaxNodesTotal < effectivePoolCapacity {
		effectivePoolCapacity = autoscaler.MaxNodesTotal
	}

	total := cluster.ControlPlanes + cluster.Workers + effectivePoolCapacity
	if total > serverLimit {
		return fmt.Errorf(
			"%w: controlPlanes(%d)+workers(%d)+effectivePoolCapacity(%d, poolCapacity(%d))=%d exceeds serverLimit(%d)", //nolint:lll
			ErrAutoscalerExceedsServerLimit,
			cluster.ControlPlanes,
			cluster.Workers,
			effectivePoolCapacity,
			poolCapacity,
			total,
			serverLimit,
		)
	}

	return nil
}

// resolveServerLimit validates and normalises the configured Hetzner server limit.
// A negative limit is rejected; zero falls back to DefaultHetznerServerLimit.
func resolveServerLimit(limit int32) (int32, error) {
	if limit < 0 {
		return 0, fmt.Errorf("%w: got %d", ErrInvalidServerLimit, limit)
	}

	if limit == 0 {
		return DefaultHetznerServerLimit, nil
	}

	return limit, nil
}

// ValidateAllowedCIDRs validates that each entry in allowedCIDRs is a valid CIDR block.
// Returns nil when the slice is empty (meaning default 0.0.0.0/0 and ::/0 behavior).
func ValidateAllowedCIDRs(cidrs []string) error {
	for idx, cidr := range cidrs {
		trimmed := strings.TrimSpace(cidr)
		if trimmed == "" {
			return fmt.Errorf("%w: entry[%d] must not be empty", ErrInvalidAllowedCIDR, idx)
		}

		_, _, err := net.ParseCIDR(trimmed)
		if err != nil {
			return fmt.Errorf("%w: entry[%d] %q: %w", ErrInvalidAllowedCIDR, idx, cidr, err)
		}
	}

	return nil
}

// ValidateOIDCConfig validates the OIDC authentication configuration.
// When OIDC is enabled (IssuerURL is set), ClientID must also be set.
// IssuerURL must use HTTPS. ClientID alone without IssuerURL is invalid.
func ValidateOIDCConfig(oidc *OIDCSpec) error {
	if oidc == nil {
		return nil
	}

	// ClientID without IssuerURL is a partial/invalid configuration
	if oidc.ClientID != "" && oidc.IssuerURL == "" {
		return fmt.Errorf("%w: issuerURL is required when clientID is set", ErrInvalidOIDCConfig)
	}

	if !oidc.Enabled() {
		return nil
	}

	if oidc.ClientID == "" {
		return fmt.Errorf("%w: clientID is required when issuerURL is set", ErrInvalidOIDCConfig)
	}

	if !strings.HasPrefix(oidc.IssuerURL, "https://") {
		return fmt.Errorf("%w: issuerURL must use HTTPS scheme", ErrInvalidOIDCConfig)
	}

	// Normalize: trim whitespace, reject empty, and deduplicate scopes.
	seen := make(map[string]struct{}, len(oidc.ExtraScopes))
	normalized := make([]string, 0, len(oidc.ExtraScopes))

	for idx, scope := range oidc.ExtraScopes {
		trimmed := strings.TrimSpace(scope)
		if trimmed == "" {
			return fmt.Errorf("%w: extraScopes[%d] must not be empty", ErrInvalidOIDCConfig, idx)
		}

		if _, dup := seen[trimmed]; !dup {
			seen[trimmed] = struct{}{}
			normalized = append(normalized, trimmed)
		}
	}

	oidc.ExtraScopes = normalized

	return nil
}

// ValidateNestedCIDRs validates that the nested cluster's pod and service CIDRs
// do not overlap with common host cluster CIDR ranges.
// Returns nil if valid, or ErrCIDROverlap describing the conflict.
func ValidateNestedCIDRs(podCIDR, serviceCIDR string) error {
	// Common host cluster CIDR ranges to check against
	hostRanges := []string{
		"10.244.0.0/16", // common host pod CIDR (Flannel, Calico default)
		"10.96.0.0/12",  // common host service CIDR (kubeadm default)
	}

	for _, cidr := range []struct {
		name  string
		value string
	}{
		{"podCidr", podCIDR},
		{"serviceCidr", serviceCIDR},
	} {
		if cidr.value == "" {
			continue
		}

		_, nestedNet, err := net.ParseCIDR(cidr.value)
		if err != nil {
			return fmt.Errorf("invalid %s %q: %w", cidr.name, cidr.value, err)
		}

		for _, hostRange := range hostRanges {
			_, hostNet, _ := net.ParseCIDR(hostRange) //nolint:errcheck // static CIDR values, parsing cannot fail

			if nestedNet.Contains(hostNet.IP) || hostNet.Contains(nestedNet.IP) {
				return fmt.Errorf(
					"%w: %s %q overlaps with common host range %s",
					ErrCIDROverlap,
					cidr.name,
					cidr.value,
					hostRange,
				)
			}
		}
	}

	return nil
}
