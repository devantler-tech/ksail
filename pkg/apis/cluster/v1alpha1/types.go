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
	Provider ProviderSpec `json:"provider,omitzero"`
	Workload WorkloadSpec `json:"workload,omitzero"`
	Chat     ChatSpec     `json:"chat,omitzero"`
}

// ProviderSpec defines provider-specific configuration for infrastructure providers.
// This separates infrastructure provider concerns (Hetzner servers, Omni SaaS) from
// cluster/distribution concerns in ClusterSpec.
type ProviderSpec struct {
	Hetzner OptionsHetzner `json:"hetzner,omitzero"`
	Omni    OptionsOmni    `json:"omni,omitzero"`
}

// ClusterSpec defines cluster-related configuration.
type ClusterSpec struct {
	DistributionConfig string        `json:"distributionConfig,omitzero"`
	Connection         Connection    `json:"connection,omitzero"`
	Distribution       Distribution  `json:"distribution,omitzero"`
	Provider           Provider      `json:"provider,omitzero"`
	CNI                CNI           `json:"cni,omitzero"`
	CSI                CSI           `json:"csi,omitzero"`
	CDI                CDI           `json:"cdi,omitzero"`
	MetricsServer      MetricsServer `json:"metricsServer,omitzero"`
	LoadBalancer       LoadBalancer  `json:"loadBalancer,omitzero"`
	CertManager        CertManager   `json:"certManager,omitzero"`
	PolicyEngine       PolicyEngine  `json:"policyEngine,omitzero"`
	LocalRegistry      LocalRegistry `json:"localRegistry,omitzero"`
	GitOpsEngine       GitOpsEngine  `json:"gitOpsEngine,omitzero"`
	SOPS               SOPS          `json:"sops,omitzero"`
	ImportImages       string        `json:"importImages,omitzero"       jsonschema:"description=Path to tar archive with container images to import after cluster creation but before component installation"` //nolint:lll // Long description required for JSON schema

	// Distribution-specific options
	Vanilla OptionsVanilla `json:"vanilla,omitzero"`
	Talos   OptionsTalos   `json:"talos,omitzero"`
}

// WorkloadSpec defines workload-related configuration.
type WorkloadSpec struct {
	SourceDirectory   string `default:"k8s"   json:"sourceDirectory,omitzero"   jsonschema_description:"Path to the directory containing Kubernetes manifests. Used as the default path by validate, watch, and push when no explicit path argument is given."`                                                                            //nolint:lll
	ValidateOnPush    bool   `default:"false" json:"validateOnPush,omitzero"    jsonschema_description:"Validate manifests against schemas before pushing (validation disabled by default)"`                                                                                                                                               //nolint:lll
	Tag               string `default:"dev"   json:"tag,omitzero"               jsonschema_description:"OCI artifact tag used for workload push and GitOps reconciliation (Flux OCIRepository and ArgoCD Application). Priority: CLI oci:// ref > this field > registry-embedded tag > dev"`                                          //nolint:lll
	KustomizationFile string `default:""      json:"kustomizationFile,omitzero" jsonschema_description:"Path to the kustomization directory relative to sourceDirectory. When set, Flux Sync.Path is configured to this path so Flux uses the specified kustomization as the entry point instead of requiring a root kustomization.yaml."` //nolint:lll
}

// ChatSpec defines AI chat assistant configuration.
type ChatSpec struct {
	Model string `json:"model,omitzero" jsonschema:"description=Chat model (empty or 'auto' for API default)"`
	// ReasoningEffort specifies the reasoning effort level for chat responses.
	// Valid values: "low", "medium", "high"
	ReasoningEffort string `json:"reasoningEffort,omitzero" jsonschema:"enum=low,enum=medium,enum=high" jsonschema_description:"Reasoning effort level for chat responses (low, medium, or high)"` //nolint:lll // Long description required for JSON schema
}

// Connection defines connection options for a KSail cluster.
type Connection struct {
	Kubeconfig string          `default:"~/.kube/config" json:"kubeconfig,omitzero"`
	Context    string          `                         json:"context,omitzero"`
	Timeout    metav1.Duration `                         json:"timeout,omitzero"`
}
