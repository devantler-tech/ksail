package v1alpha1

// ComponentState is the install outcome of a single component, as observed in the last reconcile.
// It is reported in ClusterStatus.Components[].State.
// +kubebuilder:validation:Enum=Ready;Failed
type ComponentState string

const (
	// ComponentStateReady indicates the component installed (or upgraded) successfully.
	ComponentStateReady ComponentState = "Ready"
	// ComponentStateFailed indicates the component's install failed in the last reconcile; the
	// operator retries it on the next reconcile (its Message carries the failure detail).
	ComponentStateFailed ComponentState = "Failed"
)

// String returns the string representation of the ComponentState.
func (s ComponentState) String() string {
	return string(s)
}

// ValidValues returns all valid ComponentState values as strings. It implements EnumValuer so the
// JSON schema generator can discover the allowed values automatically. CRD enum validation is
// enforced separately by the +kubebuilder:validation:Enum marker on ComponentStatus.State, because
// controller-gen does not consult this interface.
func (s ComponentState) ValidValues() []string {
	return []string{
		string(ComponentStateReady),
		string(ComponentStateFailed),
	}
}
