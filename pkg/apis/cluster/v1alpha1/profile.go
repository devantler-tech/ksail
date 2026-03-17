package v1alpha1

import (
	"fmt"
	"strings"
)

// Profile defines pre-built cluster profile templates for common workload stacks.
// Profiles bundle opinionated combinations of CNI, CSI, policy engine, and GitOps settings
// into named starting points. Individual flags still override profile defaults.
type Profile string

const (
	// ProfileDefault is the default profile (current behaviour, no-op).
	ProfileDefault Profile = "Default"
	// ProfileArgoCD pre-configures the cluster for ArgoCD-native GitOps workflows.
	// Implied defaults: GitOpsEngine=ArgoCD, CertManager=Enabled.
	ProfileArgoCD Profile = "ArgoCD"
	// ProfileMesh pre-configures the cluster for service-mesh-native applications.
	// Implied defaults: CNI=Cilium.
	ProfileMesh Profile = "Mesh"
	// ProfileObservability pre-configures the cluster with a local monitoring stack.
	// Implied defaults: MetricsServer=Enabled, CertManager=Enabled.
	ProfileObservability Profile = "Observability"
)

// Set for Profile (pflag.Value interface).
func (p *Profile) Set(value string) error {
	for _, profile := range ValidProfiles() {
		if strings.EqualFold(value, string(profile)) {
			*p = profile

			return nil
		}
	}

	return fmt.Errorf("%w: %s (valid options: %s, %s, %s, %s)",
		ErrInvalidProfile, value, ProfileDefault, ProfileArgoCD, ProfileMesh, ProfileObservability)
}

// String returns the string representation of the Profile.
func (p *Profile) String() string {
	return string(*p)
}

// Type returns the type of the Profile.
func (p *Profile) Type() string {
	return "Profile"
}

// Default returns the default value for Profile (Default).
func (p *Profile) Default() any {
	return ProfileDefault
}

// ValidValues returns all valid Profile values as strings.
func (p *Profile) ValidValues() []string {
	return []string{
		string(ProfileDefault),
		string(ProfileArgoCD),
		string(ProfileMesh),
		string(ProfileObservability),
	}
}

// ApplyProfileDefaults applies implied configuration defaults for the given profile.
// Fields that are already at a non-default/non-zero value are not overridden, allowing
// explicit settings to take precedence. When used in a CLI command context, prefer
// applyProfileDefaultsForInit (or equivalent) which checks cmd.Flags().Changed() for
// more precise override detection.
func ApplyProfileDefaults(spec *ClusterSpec, profile Profile) {
	switch profile {
	case ProfileArgoCD:
		if spec.GitOpsEngine == GitOpsEngineNone || spec.GitOpsEngine == "" {
			spec.GitOpsEngine = GitOpsEngineArgoCD
		}

		if spec.CertManager == CertManagerDisabled || spec.CertManager == "" {
			spec.CertManager = CertManagerEnabled
		}

	case ProfileMesh:
		if spec.CNI == CNIDefault || spec.CNI == "" {
			spec.CNI = CNICilium
		}

	case ProfileObservability:
		if spec.MetricsServer == MetricsServerDefault || spec.MetricsServer == "" {
			spec.MetricsServer = MetricsServerEnabled
		}

		if spec.CertManager == CertManagerDisabled || spec.CertManager == "" {
			spec.CertManager = CertManagerEnabled
		}
	}
}
