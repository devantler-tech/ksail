package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// Condition type strings reported in ClusterStatus.Conditions.
const (
	// ConditionReady is True when the cluster exists and matches the desired spec.
	ConditionReady = "Ready"
	// ConditionProgressing is True while the operator is provisioning or updating the cluster.
	ConditionProgressing = "Progressing"
	// ConditionDegraded is True when reconciliation encountered an error.
	ConditionDegraded = "Degraded"
	// ConditionComponentsReady is True when the cluster's components (CNI, CSI, metrics-server,
	// cert-manager, load-balancer, policy-engine, GitOps) are installed and reconciled. It is
	// independent of ConditionReady (which reports that the cluster itself is provisioned).
	ConditionComponentsReady = "ComponentsReady"
	// ConditionIgnoredFields is True when the Cluster resource sets CLI-only spec fields the operator
	// does not reconcile (e.g. spec.editor, spec.chat, spec.cluster.connection,
	// spec.cluster.distributionConfig, spec.workload.watch.hooks). The Cluster type is shared between
	// the ksail.yaml CLI configuration and the operator CRD, so a kubectl user can set fields that
	// only the CLI honors; this condition surfaces them explicitly (its message lists the fields)
	// rather than silently accepting and ignoring them. It is purely informational and never affects
	// ConditionReady.
	ConditionIgnoredFields = "IgnoredFields"
)

// ClusterStatus describes the observed state of a Cluster as reconciled by the KSail operator.
// It is the operator's source of truth for observed state; the imperative CLI continues to
// persist its own state under ~/.ksail/clusters/<name>/spec.json independently.
type ClusterStatus struct {
	// Phase is a high-level summary of the cluster lifecycle.
	// +kubebuilder:validation:Enum=Pending;Provisioning;Ready;Stopped;Updating;Deleting;Failed
	Phase ClusterPhase `json:"phase,omitempty"`

	// Conditions represent the latest observations of the cluster's state.
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// ObservedGeneration is the metadata.generation last processed by the operator.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// KubeconfigSecretRef points to the Secret holding the child cluster's kubeconfig.
	KubeconfigSecretRef *SecretReference `json:"kubeconfigSecretRef,omitempty"`

	// Endpoint is the stable API server URL of the provisioned cluster, when known.
	Endpoint string `json:"endpoint,omitempty"`

	// NodesReady is the number of cluster nodes reporting Ready.
	NodesReady int32 `json:"nodesReady,omitempty"`

	// NodesTotal is the total number of cluster nodes.
	NodesTotal int32 `json:"nodesTotal,omitempty"`

	// LastReconcileTime is when the operator last reconciled this cluster.
	LastReconcileTime *metav1.Time `json:"lastReconcileTime,omitempty"`

	// ExpiresAt is when a TTL-bound cluster is scheduled for automatic deletion, when a TTL is set.
	ExpiresAt *metav1.Time `json:"expiresAt,omitempty"`

	// GitOps reports observed GitOps reconciliation state for monitoring only.
	// It does NOT control the UI read-only lock, which is a deployment-level configuration.
	GitOps *GitOpsStatus `json:"gitOps,omitempty"`

	// Components reports the install outcome of each component declared in the spec (CNI, CSI,
	// metrics-server, cert-manager, load-balancer, policy-engine, GitOps), so a UI can surface
	// per-component health instead of only the aggregate ComponentsReady condition. It is empty when
	// component installation is not supported for the cluster (e.g. the provisioner exposes no
	// operator-reachable kubeconfig).
	// +listType=map
	// +listMapKey=name
	Components []ComponentStatus `json:"components,omitempty"`
}

// ComponentState is the install outcome of a single component, as observed in the last reconcile.
// +kubebuilder:validation:Enum=Ready;Failed
type ComponentState string

const (
	// ComponentStateReady indicates the component installed (or upgraded) successfully.
	ComponentStateReady ComponentState = "Ready"
	// ComponentStateFailed indicates the component's install failed in the last reconcile; the
	// operator retries it on the next reconcile (its Message carries the failure detail).
	ComponentStateFailed ComponentState = "Failed"
)

// ComponentStatus reports the install outcome of a single cluster component.
type ComponentStatus struct {
	// Name is the component's installer key (e.g. cilium, cert-manager, flux).
	Name string `json:"name"`

	// State is the component's install outcome in the last reconcile.
	State ComponentState `json:"state"`

	// Message is a human-readable detail, set to the failure reason when State is Failed.
	Message string `json:"message,omitempty"`
}

// SecretReference identifies a Secret by name and (optionally) namespace.
type SecretReference struct {
	// Name is the Secret name.
	Name string `json:"name"`
	// Namespace is the Secret namespace. Empty means the Cluster's namespace.
	Namespace string `json:"namespace,omitempty"`
}

// GitOpsStatus summarizes the GitOps engine reconciliation state observed in the child cluster.
// It is derived from the reconcile diagnostics (Flux/ArgoCD) and is observability-only.
type GitOpsStatus struct {
	// Engine is the GitOps engine detected/configured for the cluster.
	Engine GitOpsEngine `json:"engine,omitempty"`
	// Synced is true when all tracked GitOps resources report a healthy/synced state.
	Synced bool `json:"synced,omitempty"`
	// Message is a human-readable summary of the current GitOps sync state.
	Message string `json:"message,omitempty"`
	// LastSyncTime is the most recent successful sync time observed, when known.
	LastSyncTime *metav1.Time `json:"lastSyncTime,omitempty"`
}
