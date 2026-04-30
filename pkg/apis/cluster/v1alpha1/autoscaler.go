package v1alpha1

// AutoscalerConfig defines configuration for pod and node autoscaling.
type AutoscalerConfig struct {
	Pod  PodAutoscalerConfig  `json:"pod,omitzero"`
	Node NodeAutoscalerConfig `json:"node,omitzero"`
}

// PodAutoscalerConfig defines configuration for pod-level autoscaling.
type PodAutoscalerConfig struct {
	Horizontal PodAutoscalerHorizontal `json:"horizontal,omitzero"`
	Vertical   PodAutoscalerVertical   `json:"vertical,omitzero"`
}

// NodeAutoscalerConfig defines configuration for node-level autoscaling.
// When Enabled, the Cluster Autoscaler manages worker node counts dynamically.
// KSail-specified node counts serve as a baseline; the autoscaler adds and removes
// workers based on workload demand. Node-count changes via ksail cluster update
// are still applied to the Talos machine config and will take effect normally.
type NodeAutoscalerConfig struct {
	Enabled               NodeAutoscalerEnabled `json:"enabled,omitzero"`
	Pools                 []NodePool            `json:"pools,omitzero"`
	MaxNodesTotal         int32                 `json:"maxNodesTotal,omitzero"         jsonschema:"description=Maximum total nodes allowed across all node pools. Set to 0 to disable the global cap; when disabled, the effective cap is the sum of all pool max values,minimum=0"` //nolint:lll
	Expander              AutoscalerExpander    `json:"expander,omitzero"`
	ScaleDownUnneededTime string                `json:"scaleDownUnneededTime,omitzero" jsonschema:"description=How long a node should be unneeded before it is eligible for scale down (e.g. 10m)"` //nolint:lll
}

// NodePool defines a Hetzner node pool managed by the cluster autoscaler.
type NodePool struct {
	// Name is the unique identifier for this node pool (DNS-1123 label).
	Name string `json:"name"`
	// ServerType is the Hetzner server type for nodes in this pool (e.g. "cx23", "cax11").
	ServerType string `json:"serverType"`
	// Location is the Hetzner datacenter location for this pool (e.g. "fsn1", "nbg1").
	Location string `json:"location"`
	// Min is the minimum number of nodes in this pool.
	Min int32 `json:"min" jsonschema:"minimum=0"`
	// Max is the maximum number of nodes in this pool.
	Max int32 `json:"max" jsonschema:"minimum=0"`
}
