package v1alpha1

// NodeAutoscalerEnabled is the toggle enum for spec.cluster.autoscaler.node.enabled.
// It replaces the plain bool that field previously used, converging it with the
// other Enabled/Disabled toggle enums in this spec. A YAML/JSON boolean is still
// accepted on load (true -> Enabled, false -> Disabled) via the toggle bool-alias
// decode hook, so existing ksail.yaml files keep working unchanged.
type NodeAutoscalerEnabled string

const (
	// NodeAutoscalerEnabledEnabled installs and enables the Cluster Autoscaler to
	// manage worker node counts dynamically (Talos + Hetzner).
	NodeAutoscalerEnabledEnabled NodeAutoscalerEnabled = "Enabled"
	// NodeAutoscalerEnabledDisabled (default) leaves node counts managed directly by KSail.
	NodeAutoscalerEnabledDisabled NodeAutoscalerEnabled = "Disabled"
)

// ValidNodeAutoscalerEnableds returns supported node-autoscaler enabled values.
func ValidNodeAutoscalerEnableds() []NodeAutoscalerEnabled {
	return []NodeAutoscalerEnabled{NodeAutoscalerEnabledEnabled, NodeAutoscalerEnabledDisabled}
}

// Set for NodeAutoscalerEnabled (pflag.Value interface). Accepts the legacy
// boolean spelling (true/false) as a deprecation alias for Enabled/Disabled.
func (n *NodeAutoscalerEnabled) Set(value string) error {
	return setToggleEnum(n, value, ValidNodeAutoscalerEnableds(), ErrInvalidNodeAutoscaling)
}

// String returns the string representation of the NodeAutoscalerEnabled.
func (n *NodeAutoscalerEnabled) String() string {
	return string(*n)
}

// Type returns the type of the NodeAutoscalerEnabled.
func (n *NodeAutoscalerEnabled) Type() string {
	return "NodeAutoscalerEnabled"
}

// Default returns the default value for NodeAutoscalerEnabled (Disabled).
func (n *NodeAutoscalerEnabled) Default() any {
	return NodeAutoscalerEnabledDisabled
}

// ValidValues returns all valid NodeAutoscalerEnabled values as strings.
func (n *NodeAutoscalerEnabled) ValidValues() []string {
	return validValueStrings(ValidNodeAutoscalerEnableds())
}

// IsEnabled reports whether node autoscaling is enabled. It treats the empty
// (unset) value as disabled, matching the previous bool zero-value semantics.
func (n *NodeAutoscalerEnabled) IsEnabled() bool {
	return *n == NodeAutoscalerEnabledEnabled
}

// UnmarshalJSON accepts the legacy boolean spelling (true -> Enabled, false ->
// Disabled) that older ksail versions persisted to cluster state
// (~/.ksail/clusters/<name>/spec.json, when this field was a bool) and that REST
// API / operator clients may still send, in addition to the current string form.
// See unmarshalToggleEnumJSON: this keeps pre-migration spec.json and payloads
// loadable after an upgrade (the YAML path is handled by the decode hook).
func (n *NodeAutoscalerEnabled) UnmarshalJSON(data []byte) error {
	return unmarshalToggleEnumJSON(
		n,
		data,
		ValidNodeAutoscalerEnableds(),
		ErrInvalidNodeAutoscaling,
	)
}
