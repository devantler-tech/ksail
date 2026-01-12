package v1alpha1

import (
	"fmt"
	"regexp"
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
	return []Distribution{DistributionVanilla, DistributionK3s, DistributionTalos}
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
	return []CSI{CSIDefault, CSILocalPathStorage}
}

// ValidMetricsServers returns supported metrics server values.
func ValidMetricsServers() []MetricsServer {
	return []MetricsServer{
		MetricsServerDefault,
		MetricsServerEnabled,
		MetricsServerDisabled,
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
