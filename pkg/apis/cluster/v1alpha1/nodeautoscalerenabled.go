package v1alpha1

import (
	"fmt"
	"strings"
)

// NodeAutoscalerEnabled defines whether the node autoscaler is enabled.
// When Enabled, KSail defers node scaling to an external autoscaler.
type NodeAutoscalerEnabled string

const (
	// NodeAutoscalerEnabledEnabled indicates the node autoscaler is active.
	NodeAutoscalerEnabledEnabled NodeAutoscalerEnabled = "Enabled"
	// NodeAutoscalerEnabledDisabled indicates KSail manages node counts directly.
	NodeAutoscalerEnabledDisabled NodeAutoscalerEnabled = "Disabled"
)

// Set for NodeAutoscalerEnabled (pflag.Value interface).
func (n *NodeAutoscalerEnabled) Set(value string) error {
	for _, v := range ValidNodeAutoscalerEnableds() {
		if strings.EqualFold(value, string(v)) {
			*n = v

			return nil
		}
	}

	return fmt.Errorf(
		"%w: %s (valid options: %s, %s)",
		ErrInvalidNodeAutoscalerEnabled,
		value,
		NodeAutoscalerEnabledEnabled,
		NodeAutoscalerEnabledDisabled,
	)
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
	return []string{
		string(NodeAutoscalerEnabledEnabled),
		string(NodeAutoscalerEnabledDisabled),
	}
}
