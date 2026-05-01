package v1alpha1

import (
	"fmt"
	"strings"
)

// NodeAutoscaling is reserved for future external autoscaler pool support.
// Regardless of this setting, KSail always manages and diffs baseline node counts
// (controlPlanes and workers) during cluster update operations.
type NodeAutoscaling string

const (
	// NodeAutoscalingEnabled is reserved for future external autoscaler pool support.
	// Currently a no-op: KSail still manages and diffs baseline node counts regardless of this setting.
	NodeAutoscalingEnabled NodeAutoscaling = "Enabled"
	// NodeAutoscalingDisabled is the default: KSail manages node counts directly.
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
