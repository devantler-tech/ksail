package v1alpha1

import (
	"fmt"
	"strings"
)

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
