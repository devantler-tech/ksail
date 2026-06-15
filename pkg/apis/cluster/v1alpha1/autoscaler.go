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
	Enabled               bool                   `json:"enabled,omitzero"`
	Pools                 []NodePool             `json:"pools,omitzero"`
	MaxNodesTotal         int32                  `json:"maxNodesTotal,omitzero"         jsonschema:"description=Maximum total number of nodes in the cluster (control-planes + workers + autoscaler nodes). Passed verbatim to the cluster-autoscaler --max-nodes-total flag — the autoscaler evaluates it against the count of ALL nodes so this is the whole-cluster ceiling and not an autoscaler-only budget. Set to 0 to disable the global cap; growth is then bounded only by the per-pool max values and serverLimit. Should be <= serverLimit,minimum=0"` //nolint:lll
	Expander              AutoscalerExpanderList `json:"expander,omitzero"              jsonschema:"description=Node expander strategy for the cluster autoscaler. Accepts either a single value (e.g. LeastWaste) or an ordered priority list (e.g. [LeastNodes, LeastWaste]) applied as a chain — the first expander filters node groups and each later one breaks the previous tie (upstream --expander=least-nodes,least-waste)."`                                                                                                                             //nolint:lll
	ScaleDownUnneededTime string                 `json:"scaleDownUnneededTime,omitzero" jsonschema:"description=How long a node should be unneeded before it is eligible for scale down (e.g. 10m)"`                                                                                                                                                                                                                                                                                                                                                               //nolint:lll
}

// NodePool defines a Hetzner node pool managed by the cluster autoscaler.
type NodePool struct {
	// Name is the unique identifier for this node pool (DNS-1123 label).
	Name string `json:"name" jsonschema:"minLength=1,maxLength=63,pattern=^[a-z0-9]([-a-z0-9]*[a-z0-9])?$"`
	// ServerType is the Hetzner server type for nodes in this pool (e.g. "cx23", "cax11").
	ServerType string `json:"serverType" jsonschema:"minLength=1"`
	// Location is the Hetzner datacenter location for this pool (e.g. "fsn1", "nbg1").
	Location string `json:"location" jsonschema:"minLength=1"`
	// Min is the minimum number of nodes in this pool.
	Min int32 `json:"min" jsonschema:"minimum=0"`
	// Max is the maximum number of nodes in this pool.
	Max int32 `json:"max" jsonschema:"minimum=0"`
	// Labels are Kubernetes node labels applied to every node provisioned in this
	// pool. They are baked into the pool's Talos worker config (machine.nodeLabels)
	// so they land on the real Node object, and are also attributed to the pool's
	// scale-from-zero template so the autoscaler scales the pool for pods that
	// select these labels. Keys must be valid Kubernetes label keys.
	Labels map[string]string `json:"labels,omitzero" jsonschema:"description=Kubernetes node labels applied to every node in this pool (via Talos machine.nodeLabels and the autoscaler scale-from-zero template)."` //nolint:lll
	// Taints are Kubernetes node taints applied to every node provisioned in this
	// pool. They are baked into the pool's Talos worker config (machine.nodeTaints)
	// so they land on the real Node object, and are also attributed to the pool's
	// scale-from-zero template so the autoscaler only scales the pool for pods that
	// tolerate the taints.
	Taints []NodePoolTaint `json:"taints,omitzero" jsonschema:"description=Kubernetes node taints applied to every node in this pool (via Talos machine.nodeTaints and the autoscaler scale-from-zero template)."` //nolint:lll
}

// NodePoolTaint defines a Kubernetes node taint applied to every node in an
// autoscaler node pool.
type NodePoolTaint struct {
	// Key is the taint key. Must be a valid Kubernetes label key (an optional
	// DNS-subdomain prefix followed by a name segment).
	Key string `json:"key" jsonschema:"minLength=1"`
	// Value is the optional taint value.
	Value string `json:"value,omitzero"`
	// Effect is the scheduling effect: NoSchedule, PreferNoSchedule, or NoExecute.
	Effect TaintEffect `json:"effect"`
}
