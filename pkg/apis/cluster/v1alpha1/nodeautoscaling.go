package v1alpha1

import (
	"fmt"
	"strings"
)

// NodeAutoscaling is a deprecated alias for [NodeAutoscalerEnabled].
// Setting NodeAutoscalingEnabled migrates to NodeAutoscalerEnabledEnabled at load time.
// Regardless of this setting, KSail always manages and diffs baseline node counts
// (controlPlanes and workers) during cluster update operations.
//
// Deprecated: use [NodeAutoscalerEnabled] / spec.cluster.autoscaler.node.enabled instead.
type NodeAutoscaling string

const (
	// NodeAutoscalingEnabled is a deprecated alias for [NodeAutoscalerEnabledEnabled].
	// When set, installs and enables Cluster Autoscaler for node pool autoscaling on supported
	// configurations (Talos + Hetzner). Baseline node counts (controlPlanes/workers) are
	// still reconciled by KSail regardless of this setting.
	//
	// Deprecated: use autoscaler.node.enabled instead.
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
