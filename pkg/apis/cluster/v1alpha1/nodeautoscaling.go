package v1alpha1

import (
	"fmt"
	"strings"
)

// NodeAutoscaling defines whether an external node autoscaler manages cluster node counts.
// When Enabled, ksail cluster update skips controlPlanes and workers diffs and scaling
// operations to avoid conflicts with the autoscaler.
type NodeAutoscaling string

const (
	// NodeAutoscalingEnabled indicates an external autoscaler manages node counts.
	// Update operations will not modify controlPlanes or workers.
	NodeAutoscalingEnabled NodeAutoscaling = "Enabled"
	// NodeAutoscalingDisabled indicates KSail manages node counts directly.
	NodeAutoscalingDisabled NodeAutoscaling = "Disabled"
)

// Set for NodeAutoscaling (pflag.Value interface).
func (n *NodeAutoscaling) Set(value string) error {
	for _, v := range ValidNodeAutoscalings() {
		if strings.EqualFold(value, string(v)) {
			*n = v

			return nil
		}
	}

	return fmt.Errorf(
		"%w: %s (valid options: %s, %s)",
		ErrInvalidNodeAutoscaling,
		value,
		NodeAutoscalingEnabled,
		NodeAutoscalingDisabled,
	)
}

// String returns the string representation of the NodeAutoscaling.
func (n *NodeAutoscaling) String() string {
	return string(*n)
}

// Type returns the type of the NodeAutoscaling.
func (n *NodeAutoscaling) Type() string {
	return "NodeAutoscaling"
}

// Default returns the default value for NodeAutoscaling (Disabled).
func (n *NodeAutoscaling) Default() any {
	return NodeAutoscalingDisabled
}

// ValidValues returns all valid NodeAutoscaling values as strings.
func (n *NodeAutoscaling) ValidValues() []string {
	return []string{string(NodeAutoscalingEnabled), string(NodeAutoscalingDisabled)}
}
