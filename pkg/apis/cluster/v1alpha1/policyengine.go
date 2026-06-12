package v1alpha1

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

// ValidPolicyEngines returns supported policy engine values.
func ValidPolicyEngines() []PolicyEngine {
	return []PolicyEngine{
		PolicyEngineNone,
		PolicyEngineKyverno,
		PolicyEngineGatekeeper,
	}
}

// Set for PolicyEngine (pflag.Value interface).
func (p *PolicyEngine) Set(value string) error {
	return setEnum(p, value, ValidPolicyEngines(), ErrInvalidPolicyEngine)
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
	return validValueStrings(ValidPolicyEngines())
}
