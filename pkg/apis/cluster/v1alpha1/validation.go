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
	return []Provider{ProviderDocker, ProviderHetzner}
}

// ValidPlacementGroupStrategies returns supported placement group strategy values.
func ValidPlacementGroupStrategies() []PlacementGroupStrategy {
	return []PlacementGroupStrategy{PlacementGroupStrategyNone, PlacementGroupStrategySpread}
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
	if provider == ProviderHetzner {
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
	if provider == ProviderHetzner && !registry.IsExternal() {
		return ErrLocalRegistryNotSupported
	}

	return nil
}
