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
	// ProfileGitOps is a profile for GitOps workflows.
	// It sets GitOpsEngine=Flux and enables a local OCI registry for image caching.
	ProfileGitOps Profile = "GitOps"
	// ProfileSecurity is a profile for security-hardened clusters.
	// It sets CNI=Cilium, PolicyEngine=Kyverno, and CertManager=Enabled.
	ProfileSecurity Profile = "Security"
)

// Set for Profile (pflag.Value interface).
func (p *Profile) Set(value string) error {
	for _, profile := range ValidProfiles() {
		if strings.EqualFold(value, string(profile)) {
			*p = profile

			return nil
		}
	}

	return fmt.Errorf("%w: %s (valid options: %s)",
		ErrInvalidProfile, value, strings.Join([]string{
			string(ProfileDefault),
			string(ProfileGitOps),
			string(ProfileSecurity),
		}, ", "))
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
	return []string{string(ProfileDefault), string(ProfileGitOps), string(ProfileSecurity)}
}
