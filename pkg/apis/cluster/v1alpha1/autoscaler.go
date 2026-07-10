package v1alpha1

// AutoscalerConfig defines configuration for pod and node autoscaling.
type AutoscalerConfig struct {
	// Pod configures pod-level autoscaling (horizontal and vertical).
	Pod PodAutoscalerConfig `json:"pod,omitzero"`
	// Node configures node-level autoscaling via the Cluster Autoscaler.
	Node NodeAutoscalerConfig `json:"node,omitzero"`
}

// PodAutoscalerConfig defines configuration for pod-level autoscaling.
type PodAutoscalerConfig struct {
	// Horizontal controls Horizontal Pod Autoscaler (HPA) support.
	Horizontal PodAutoscalerHorizontal `json:"horizontal,omitzero"`
	// Vertical controls Vertical Pod Autoscaler (VPA) support.
	Vertical PodAutoscalerVertical `json:"vertical,omitzero"`
}

// NodeAutoscalerConfig defines configuration for node-level autoscaling.
// When Enabled, the Cluster Autoscaler manages worker node counts dynamically.
// KSail-specified node counts serve as a baseline; the autoscaler adds and removes
// workers based on workload demand. Node-count changes via ksail cluster update
// are still applied to the Talos machine config and will take effect normally.
type NodeAutoscalerConfig struct {
	// Enabled controls whether the Cluster Autoscaler is installed to manage
	// worker node counts dynamically (Enabled or Disabled). A YAML boolean is
	// still accepted on load (true -> Enabled, false -> Disabled).
	Enabled NodeAutoscalerEnabled `json:"enabled,omitzero" jsonschema:"description=Whether the Cluster Autoscaler is installed to manage worker node counts dynamically (Enabled or Disabled). A YAML boolean is accepted as an alias (true=Enabled and false=Disabled)."` //nolint:lll
	// Pools defines the node pools the Cluster Autoscaler may scale (Hetzner only).
	Pools []NodePool `json:"pools,omitzero"`
	// MaxNodesTotal caps the total number of nodes in the cluster
	// (control-planes + workers + autoscaler nodes). 0 disables the global cap.
	MaxNodesTotal int32 `json:"maxNodesTotal,omitzero" jsonschema:"description=Maximum total number of nodes in the cluster (control-planes + workers + autoscaler nodes). Passed verbatim to the cluster-autoscaler --max-nodes-total flag — the autoscaler evaluates it against the count of ALL nodes so this is the whole-cluster ceiling and not an autoscaler-only budget. Set to 0 to disable the global cap; growth is then bounded only by the per-pool max values and serverLimit. Should be <= serverLimit,minimum=0"` //nolint:lll
	// Expander selects the Cluster Autoscaler expander strategy. It accepts a
	// single value (e.g. LeastWaste) or an ordered priority list
	// (e.g. [LeastNodes, LeastWaste]) applied as a chain.
	Expander AutoscalerExpanderList `json:"expander,omitzero" jsonschema:"description=Node expander strategy for the cluster autoscaler. Accepts either a single value (e.g. LeastWaste) or an ordered priority list (e.g. [LeastNodes LeastWaste]) applied as a chain — the first expander filters node groups and each later one breaks the previous tie (the upstream comma-separated --expander chain)."` //nolint:lll
	// ScaleDownUnneededTime is how long a node must be unneeded before it is
	// eligible for scale down (e.g. "10m").
	ScaleDownUnneededTime string `json:"scaleDownUnneededTime,omitzero" jsonschema:"description=How long a node should be unneeded before it is eligible for scale down (e.g. 10m)"` //nolint:lll
	// ScaleDownUtilizationThreshold is the node resource-utilization ratio
	// (0.0–1.0, computed over requests) at or below which the Cluster Autoscaler
	// considers a node for scale down (upstream --scale-down-utilization-threshold,
	// default 0.5). Passed verbatim as a string; leave empty to inherit the upstream
	// default. For agent-heavy clusters prefer ignoreDaemonsetsUtilization, which
	// excludes DaemonSet requests from this calculation entirely.
	ScaleDownUtilizationThreshold string `json:"scaleDownUtilizationThreshold,omitzero" jsonschema_description:"Node resource-utilization ratio (0.0–1.0, computed over requests) at or below which the Cluster Autoscaler considers a node for scale down (upstream --scale-down-utilization-threshold, default 0.5). Passed verbatim; leave empty to inherit the upstream default. For agent-heavy clusters prefer ignoreDaemonsetsUtilization, which excludes DaemonSet requests from this calculation. Ignored unless the node autoscaler is installed (Talos on Hetzner with enabled: true)."` //nolint:lll
	// CapacityBuffers enables the Cluster Autoscaler capacity-buffers feature:
	// KSail installs the CapacityBuffer CRD and turns on the buffer controller
	// and pod-injection flags, so CapacityBuffer resources reserve scale-up
	// headroom as virtual (pod-less) chunks — a native replacement for
	// low-priority balloon-pod overprovisioning.
	CapacityBuffers bool `json:"capacityBuffers,omitzero" jsonschema:"description=Enable the Cluster Autoscaler capacity-buffers feature: KSail installs the CapacityBuffer CRD (capacitybuffers.autoscaling.x-k8s.io) and enables the buffer controller and pod-injection flags. CapacityBuffer resources then reserve scale-up headroom as virtual (pod-less) chunks simulated in autoscaler memory — a native replacement for low-priority balloon-pod overprovisioning. Ignored unless the node autoscaler is installed (Talos on Hetzner with enabled: true)"` //nolint:lll
	// IgnoreDaemonsetsUtilization excludes DaemonSet pods from a node's
	// resource-utilization calculation when the autoscaler decides whether a node
	// is unneeded (upstream --ignore-daemonsets-utilization, off by default). Set
	// this when every DaemonSet is a system component whose per-node overhead
	// should not, on its own, keep an otherwise-empty node above the scale-down
	// threshold — otherwise heavy node agents (CNI, storage, observability) can
	// pin every autoscaler node as "utilized" and prevent scale-down entirely.
	IgnoreDaemonsetsUtilization bool `json:"ignoreDaemonsetsUtilization,omitzero" jsonschema_description:"Exclude DaemonSet pods from a node's resource-utilization calculation when the Cluster Autoscaler decides whether a node is unneeded (upstream --ignore-daemonsets-utilization, off by default). Enable this when DaemonSets are system components (CNI, CSI, observability, security agents) whose per-node overhead should not keep an otherwise-empty node above the scale-down utilization threshold. Ignored unless the node autoscaler is installed (Talos on Hetzner with enabled: true)."` //nolint:lll
	// SkipNodesWithLocalStorage controls whether the Cluster Autoscaler refuses to
	// scale down a node that runs a pod with local storage (emptyDir, hostPath, or
	// a local PersistentVolume). Upstream defaults to true (never remove such
	// nodes). Set false to allow scale-down of nodes whose only local storage is
	// ephemeral scratch — required for overflow nodes to ever drain, since
	// emptyDir is pervasive. A pointer so an explicit false is preserved and only
	// an unset value inherits the upstream default. Ignored unless the node
	// autoscaler is installed (Talos on Hetzner with enabled: true).
	SkipNodesWithLocalStorage *bool `json:"skipNodesWithLocalStorage,omitzero" jsonschema_description:"Whether the Cluster Autoscaler refuses to scale down a node running a pod with local storage (emptyDir, hostPath, or a local PersistentVolume). Upstream --skip-nodes-with-local-storage defaults to true. Set false to let nodes whose only local storage is ephemeral scratch (emptyDir) be removed — required for overflow nodes to drain, since emptyDir is pervasive. Ensure durable data lives on real PVCs first. Ignored unless the node autoscaler is installed (Talos on Hetzner with enabled: true)."` //nolint:lll
	// SkipNodesWithSystemPods controls whether the Cluster Autoscaler refuses to
	// scale down a node that runs a non-DaemonSet kube-system pod without a
	// controlling PodDisruptionBudget. Upstream defaults to true. Set false to let
	// overflow nodes hosting movable system Deployments (metrics-server, CSI
	// controllers, relays) drain — verify those components tolerate eviction and
	// carry PDBs. A pointer so an explicit false is preserved and only an unset
	// value inherits the upstream default. Ignored unless the node autoscaler is
	// installed (Talos on Hetzner with enabled: true).
	SkipNodesWithSystemPods *bool `json:"skipNodesWithSystemPods,omitzero" jsonschema_description:"Whether the Cluster Autoscaler refuses to scale down a node running a non-DaemonSet kube-system pod that has no controlling PodDisruptionBudget. Upstream --skip-nodes-with-system-pods defaults to true. Set false to let overflow nodes hosting movable system Deployments drain — confirm those components tolerate eviction and carry PDBs first. Ignored unless the node autoscaler is installed (Talos on Hetzner with enabled: true)."` //nolint:lll
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
