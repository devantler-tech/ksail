package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

const (
	// Group is the API group for KSail.
	Group = "ksail.io"
	// Version is the API version for KSail.
	Version = "v1alpha1"
	// Kind is the kind for KSail clusters.
	Kind = "Cluster"
	// APIVersion is the full API version for KSail.
	APIVersion = Group + "/" + Version
)

// --- Core Types ---

// Cluster represents a KSail cluster configuration including API metadata and desired state.
// It contains TypeMeta for API versioning information and Spec for the cluster specification.
type Cluster struct {
	metav1.TypeMeta `json:",inline" mapstructure:",squash"`

	Spec Spec `json:"spec,omitzero" mapstructure:"spec,omitempty"`
}

// Spec defines the desired state of a KSail cluster.
type Spec struct {
	Editor   string       `json:"editor,omitzero"   jsonschema:"description=Editor command for interactive workflows (e.g. code --wait)"` //nolint:lll
	Cluster  ClusterSpec  `json:"cluster,omitzero"`
	Workload WorkloadSpec `json:"workload,omitzero"`
}

// ClusterSpec defines cluster-related configuration.
type ClusterSpec struct {
	DistributionConfig string        `json:"distributionConfig,omitzero"`
	Connection         Connection    `json:"connection,omitzero"`
	Distribution       Distribution  `json:"distribution,omitzero"`
	CNI                CNI           `json:"cni,omitzero"`
	CSI                CSI           `json:"csi,omitzero"`
	MetricsServer      MetricsServer `json:"metricsServer,omitzero"`
	CertManager        CertManager   `json:"certManager,omitzero"`
	PolicyEngine       PolicyEngine  `json:"policyEngine,omitzero"`
	LocalRegistry      LocalRegistry `json:"localRegistry,omitzero"`
	GitOpsEngine       GitOpsEngine  `json:"gitOpsEngine,omitzero"`

	// Distribution-specific options (previously under Options)
	Kind  OptionsKind  `json:"kind,omitzero"`
	K3d   OptionsK3d   `json:"k3d,omitzero"`
	Talos OptionsTalos `json:"talos,omitzero"`

	// CNI-specific options
	Cilium OptionsCilium `json:"cilium,omitzero"`
	Calico OptionsCalico `json:"calico,omitzero"`

	// Tool-specific options
	Flux              OptionsFlux          `json:"flux,omitzero"`
	ArgoCD            OptionsArgoCD        `json:"argocd,omitzero"`
	LocalRegistryOpts OptionsLocalRegistry `json:"localRegistryOptions,omitzero"`
	Helm              OptionsHelm          `json:"helm,omitzero"`
	Kustomize         OptionsKustomize     `json:"kustomize,omitzero"`
}

// WorkloadSpec defines workload-related configuration.
type WorkloadSpec struct {
	SourceDirectory string `default:"k8s" json:"sourceDirectory,omitzero"`
	ValidateOnPush  bool   `              json:"validateOnPush,omitzero"`
}

// Connection defines connection options for a KSail cluster.
type Connection struct {
	Kubeconfig string          `default:"~/.kube/config" json:"kubeconfig,omitzero"`
	Context    string          `                         json:"context,omitzero"`
	Timeout    metav1.Duration `                         json:"timeout,omitzero"`
}
