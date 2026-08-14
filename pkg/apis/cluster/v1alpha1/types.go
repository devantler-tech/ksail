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
// It contains TypeMeta for API versioning information, ObjectMeta for the cluster name and
// standard Kubernetes object metadata, Spec for the desired state, and Status for the
// observed state when reconciled by the KSail operator.
//
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=ksail
// +kubebuilder:printcolumn:name="Distribution",type=string,JSONPath=`.spec.cluster.distribution`
// +kubebuilder:printcolumn:name="Provider",type=string,JSONPath=`.spec.cluster.provider`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Endpoint",type=string,JSONPath=`.status.endpoint`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type Cluster struct {
	metav1.TypeMeta   `json:",inline"            mapstructure:",squash"`
	metav1.ObjectMeta `json:"metadata,omitempty" mapstructure:"metadata,omitempty"`

	Spec   Spec          `json:"spec,omitzero"    mapstructure:"spec,omitempty"`
	Status ClusterStatus `json:"status,omitempty" mapstructure:"-"`
}

// ClusterList contains a list of Cluster resources. It is required by controller-runtime
// for list/watch operations on the Cluster custom resource.
//
// +kubebuilder:object:root=true
type ClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []Cluster `json:"items"`
}

// Spec defines the desired state of a KSail cluster.
type Spec struct {
	// Editor is the editor command launched for interactive workflows (e.g. "code --wait").
	// CLI-only; ignored by the operator (the Cluster CRD shares this type but never reads it).
	Editor string `json:"editor,omitzero" jsonschema_description:"Editor command for interactive workflows (e.g. code --wait). CLI-only; ignored by the operator."` //nolint:lll
	// Cluster configures the Kubernetes cluster KSail manages: distribution,
	// provider, components, and connection settings.
	Cluster ClusterSpec `json:"cluster,omitzero"`
	// Provider holds infrastructure-provider-specific options
	// (Hetzner, Omni, AWS, GCP, Azure, and the Kubernetes provider for nested clusters).
	Provider ProviderSpec `json:"provider,omitzero"`
	// Workload configures workload management: the manifest source directory,
	// OCI push and validation settings, and GitOps bootstrap options.
	Workload WorkloadSpec `json:"workload,omitzero"`
	// Chat configures the KSail AI chat assistant.
	// CLI-only; ignored by the operator (the Cluster CRD shares this type but never reads it).
	Chat ChatSpec `json:"chat,omitzero"`
}

// ProviderSpec defines provider-specific configuration for infrastructure providers.
// This separates infrastructure provider concerns (Hetzner servers, Omni SaaS) from
// cluster/distribution concerns in ClusterSpec.
type ProviderSpec struct {
	// Hetzner holds options for the Hetzner Cloud provider.
	Hetzner OptionsHetzner `json:"hetzner,omitzero"`
	// Omni holds options for the Sidero Omni provider.
	Omni OptionsOmni `json:"omni,omitzero"`
	// AWS holds options for the AWS provider used by the EKS distribution.
	AWS OptionsAWS `json:"aws,omitzero"`
	// GCP holds options for the Google Cloud provider used by the GKE distribution.
	GCP OptionsGCP `json:"gcp,omitzero"`
	// Azure holds options for the Microsoft Azure provider used by the AKS distribution.
	Azure OptionsAzure `json:"azure,omitzero"`
	// Kubernetes holds options for the Kubernetes provider, which runs nested
	// clusters as pods inside an existing host cluster.
	Kubernetes OptionsKubernetes `json:"kubernetes,omitzero"`
}

// ClusterSpec defines cluster-related configuration.
type ClusterSpec struct {
	// DistributionConfig is the path to the distribution's own configuration file or directory
	// (e.g. kind.yaml, k3d.yaml, talos/, vcluster.yaml, kwok/, eks.yaml, gke.yaml, or aks.yaml).
	// When empty, KSail uses the distribution's default configuration path.
	// CLI-only (a local file path on the CLI host); ignored by the operator.
	DistributionConfig string `json:"distributionConfig,omitzero" jsonschema_description:"Path to the distribution's own configuration file or directory (e.g. kind.yaml, k3d.yaml, talos/, vcluster.yaml, kwok/, eks.yaml, gke.yaml, or aks.yaml). CLI-only; ignored by the operator."` //nolint:lll
	// Connection defines how KSail connects to the cluster: the kubeconfig path,
	// context name, and operation timeout.
	// CLI-only (local kubeconfig path/context); ignored by the operator.
	Connection Connection `json:"connection,omitzero"`
	// Distribution selects the Kubernetes distribution to provision: Vanilla (Kind),
	// K3s (K3d), Talos, VCluster, KWOK (simulated), EKS (AWS), GKE (Google Cloud),
	// or AKS (Azure).
	Distribution Distribution `json:"distribution,omitzero"`
	// Provider selects the infrastructure that runs the cluster nodes: Docker,
	// Hetzner, Omni, AWS, GCP, Azure, or Kubernetes (nested clusters inside an
	// existing cluster). Each distribution supports a subset of providers; when
	// empty, KSail uses the distribution's default provider.
	Provider Provider `json:"provider,omitzero"`
	// CNI selects the Container Network Interface plugin. Default keeps the
	// distribution's built-in CNI; Cilium or Calico install that CNI instead.
	CNI CNI `json:"cni,omitzero"`
	// CSI controls Container Storage Interface support. Default keeps the
	// distribution's behavior; Enabled installs a CSI driver
	// (local-path-provisioner, or Hetzner CSI on Hetzner); Disabled installs none.
	CSI CSI `json:"csi,omitzero"`
	// CDI controls Container Device Interface support in the container runtime
	// (Default, Enabled, or Disabled).
	CDI CDI `json:"cdi,omitzero"`
	// MetricsServer controls metrics-server installation. Default keeps the
	// distribution's behavior; Enabled or Disabled override it.
	MetricsServer MetricsServer `json:"metricsServer,omitzero"`
	// LoadBalancer controls load-balancer support. Default keeps the
	// distribution and provider behavior; Enabled or Disabled override it.
	LoadBalancer LoadBalancer `json:"loadBalancer,omitzero"`
	// CertManager controls whether cert-manager is installed (Enabled or Disabled).
	CertManager CertManager `json:"certManager,omitzero"`
	// ImageVerification controls container-image signature verification scaffolding
	// for every supported distribution (not just Talos):
	//   - Talos: scaffolds a native ImageVerificationConfig document (Talos 1.13+).
	//   - Vanilla (Kind): injects a containerd image-verifier plugin patch.
	//   - K3s (K3d): scaffolds a containerd config template with the image-verifier
	//     plugin and mounts it into the node containers.
	// Enabled requires verifier binaries (and typically policy) to be present in the
	// node image bin_dir. Disabled (default) skips it.
	// Supersedes spec.cluster.talos.imageVerification (deprecated; aliased on load).
	ImageVerification ImageVerification `json:"imageVerification,omitzero" jsonschema_description:"Container-image signature verification scaffolding for all distributions: Talos scaffolds an ImageVerificationConfig document (1.13+); Vanilla/Kind injects a containerd verifier plugin patch; K3s/K3d scaffolds a containerd config template and mounts it into node containers. Requires verifier binaries (and typically policy) in the node image bin_dir. Disabled skips it."` //nolint:lll
	// PolicyEngine selects the policy engine to install: None, Kyverno, or Gatekeeper.
	PolicyEngine PolicyEngine `json:"policyEngine,omitzero"`
	// LocalRegistry configures the host-local OCI registry (or an external
	// registry for cloud providers) used by GitOps workflows.
	LocalRegistry LocalRegistry `json:"localRegistry,omitzero"`
	// GitOpsEngine selects the GitOps engine KSail bootstraps: None, Flux, or ArgoCD.
	GitOpsEngine GitOpsEngine `json:"gitOpsEngine,omitzero"`
	// SOPS configures automatic creation of the SOPS Age secret used to decrypt
	// encrypted manifests in the cluster.
	SOPS SOPS `json:"sops,omitzero"`
	// NodeAutoscaling is a deprecated alias for spec.cluster.autoscaler.node.enabled
	// and is migrated on load. Do not set both nodeAutoscaling and autoscaler.
	NodeAutoscaling NodeAutoscaling `json:"nodeAutoscaling,omitzero" jsonschema_description:"Deprecated. Use autoscaler.node.enabled instead. Do not set both nodeAutoscaling and autoscaler."` //nolint:lll
	// Autoscaler defines pod and node autoscaling configuration.
	// Supersedes spec.cluster.nodeAutoscaling (deprecated; aliased on load).
	Autoscaler   AutoscalerConfig `json:"autoscaler,omitzero"   jsonschema_description:"Pod and node autoscaling configuration (supersedes deprecated nodeAutoscaling)"`                               //nolint:lll // Long description required for JSON schema
	ImportImages string           `json:"importImages,omitzero" jsonschema_description:"Path to tar archive with container images to import after cluster creation but before component installation"` //nolint:lll // Long description required for JSON schema
	// ControlPlanes is the number of control-plane nodes (default: 1).
	// Provider/distribution-agnostic: applies to Vanilla (Kind), K3s (K3d), Talos, and VCluster.
	// Supersedes spec.cluster.talos.controlPlanes (deprecated; aliased on load).
	ControlPlanes int32 `default:"1" json:"controlPlanes,omitzero" jsonschema:"minimum=0" jsonschema_description:"Number of control-plane nodes to create for the cluster (provider/distribution-agnostic)"` //nolint:lll
	// Workers is the number of worker nodes (default: 0).
	// Provider/distribution-agnostic: applies to Vanilla (Kind), K3s (K3d), Talos, and VCluster.
	// When 0 on Talos, scheduling is allowed on control-plane nodes.
	// Supersedes spec.cluster.talos.workers (deprecated; aliased on load).
	Workers int32 `json:"workers,omitzero" jsonschema:"minimum=0" jsonschema_description:"Number of worker nodes to create for the cluster (provider/distribution-agnostic)"` //nolint:lll

	// KubernetesVersion pins the Kubernetes version to deploy. Accepts values with
	// or without the "v" prefix (e.g., "v1.32.0" or "1.32.0"). Honored by the Talos
	// distribution (Docker/Hetzner); for Kind, K3d, and EKS the version is set in the
	// distribution config (kind.yaml/k3d.yaml/eks.yaml) instead.
	//
	// When set, KSail reconciles toward this version: `cluster create` provisions at
	// it and `cluster update` upgrades the cluster toward it (skipping downgrades).
	// For brand-new Talos clusters an unset value uses a built-in default capped to a
	// version compatible with the pinned Talos release (spec.cluster.talos.version),
	// so a pinned older Talos version is never paired with a Kubernetes version it
	// cannot run.
	//
	// When unset, `cluster update` follows the latest stable Kubernetes version
	// available in the OCI registry (an in-place rolling upgrade for Talos; a
	// confirmation-gated recreation for Kind/K3d). Override per invocation with the
	// --kubernetes-version flag (precedence: flag > env > config > default).
	KubernetesVersion string `json:"kubernetesVersion,omitzero" jsonschema_description:"Kubernetes version to deploy. When set: cluster create/update reconcile toward it. When unset: cluster update follows the latest stable version and new clusters use a default compatible with the pinned Talos version."` //nolint:lll

	// OIDC defines OIDC authentication configuration.
	// When issuerURL is set, KSail configures the API server with OIDC flags
	// and sets up kubeconfig with exec-based OIDC credentials.
	OIDC OIDCSpec `json:"oidc,omitzero" jsonschema_description:"OIDC authentication configuration for the API server and kubeconfig"` //nolint:lll

	// Distribution-specific options

	// Vanilla holds options specific to the Vanilla (Kind) distribution.
	Vanilla OptionsVanilla `json:"vanilla,omitzero"`
	// Talos holds options specific to the Talos distribution.
	Talos OptionsTalos `json:"talos,omitzero"`
	// EKS holds options specific to the EKS distribution.
	EKS OptionsEKS `json:"eks,omitzero"`
}

// TotalNodeCount returns the total number of nodes (control-plane + workers).
func (c ClusterSpec) TotalNodeCount() int32 {
	return c.ControlPlanes + c.Workers
}

// WorkloadSpec defines workload-related configuration.
type WorkloadSpec struct {
	SourceDirectory   string           `default:"k8s"   json:"sourceDirectory,omitzero"   jsonschema_description:"Path to the directory containing Kubernetes manifests. Used as the default path by validate, watch, and push when no explicit path argument is given."`                                                                                                      //nolint:lll
	ValidateOnPush    bool             `default:"false" json:"validateOnPush,omitzero"    jsonschema_description:"Validate manifests against schemas before pushing (validation disabled by default)"`                                                                                                                                                                         //nolint:lll
	Tag               string           `default:"dev"   json:"tag,omitzero"               jsonschema_description:"OCI artifact tag used for workload push and GitOps reconciliation (Flux OCIRepository and ArgoCD Application). Push priority: CLI oci:// ref > this field > registry-embedded tag > dev. Reconciliation priority: this field > registry-embedded tag > dev"` //nolint:lll
	KustomizationFile string           `default:""      json:"kustomizationFile,omitzero" jsonschema_description:"Path to the kustomization directory relative to sourceDirectory. When set, Flux Sync.Path is configured to this path so Flux uses the specified kustomization as the entry point instead of requiring a root kustomization.yaml."`                           //nolint:lll
	Flux              FluxConfig       `                json:"flux,omitzero"              jsonschema_description:"Flux bootstrap configuration: operator/distribution version pins and signature verification for the generated OCIRepository. Empty values use KSail's pinned versions; a GitOps repo that declares these becomes the steady-state owner."`                   //nolint:lll
	Watch             WatchConfig      `                json:"watch,omitzero"             jsonschema_description:"Configuration for the workload watch command (pre-apply hooks, etc.)"`                                                                                                                                                                                       //nolint:lll
	Validation        ValidationConfig `                json:"validation,omitzero"        jsonschema_description:"Configuration for the workload validate command (additional kinds to skip, etc.)."`                                                                                                                                                                          //nolint:lll
	Scan              ScanConfig       `                json:"scan,omitzero"              jsonschema_description:"Configuration for the workload scan command (Kubescape exceptions, frameworks, compliance threshold) so 'ksail workload scan' (no args) can act as a turnkey CI gate."`                                                                                      //nolint:lll
}

// ValidationConfig defines configuration for the workload validate command.
type ValidationConfig struct {
	// SkipKinds lists additional Kubernetes kinds to skip during validation.
	// Kubernetes Secrets are skipped by default via --skip-secrets; this is for
	// skipping CRDs whose schema in the CRDs-catalog is stale or missing, which
	// kubeconform would otherwise reject (e.g. valid newer fields flagged as
	// "additional properties not allowed").
	SkipKinds []string `json:"skipKinds,omitzero" jsonschema_description:"Additional Kubernetes kinds to skip during 'ksail workload validate' (Secrets are skipped by default via --skip-secrets). Use for CRDs whose CRDs-catalog schema is stale or missing, which kubeconform would otherwise reject."` //nolint:lll

	// SchemaLocations lists additional kubeconform schema locations (local
	// directories or URL templates) for CRDs absent from the CRDs-catalog, so
	// they can be validated against a supplied schema rather than skipped.
	SchemaLocations []string `json:"schemaLocations,omitzero" jsonschema_description:"Additional kubeconform schema locations (local directories or URL templates) for 'ksail workload validate'. Appended after the built-in Kubernetes schemas and the CRDs-catalog, so CRDs absent from the catalog can be validated against a supplied schema instead of being skipped via skipKinds. A directory is searched using kubeconform's default '{{.ResourceKind}}{{.KindSuffix}}.json' layout; a URL/path template (e.g. 'schemas/{{.Group}}/{{.ResourceKind}}_{{.ResourceAPIVersion}}.json') is used verbatim. Merged with --schema-location."` //nolint:lll

	// HelmRender controls whether HelmReleases are rendered before validation.
	HelmRender *bool `json:"helmRender,omitzero" jsonschema_description:"Render HelmReleases (Kustomize + Helm) before 'ksail workload validate' so the actually-applied manifests are validated rather than the opaque HelmRelease CR. Charts are resolved from the OCIRepository/HelmRepository sources in the same directory and rendered in-process; releases that cannot be rendered offline fall back to validating the CR. Defaults to true. Override per-run with --skip-helm-render."` //nolint:lll

	// Rules is the path to a YAML CEL rules file evaluated during validation, so
	// rule validation can be a turnkey CI gate declared once in ksail.yaml rather
	// than requiring --rules on every invocation. The --rules flag overrides it.
	Rules string `json:"rules,omitzero" jsonschema_description:"Path to a YAML CEL rules file for 'ksail workload validate'. Each rule's CEL expression is evaluated against every rendered document (bound to the 'object' variable); an error-severity violation fails validation, a warning-severity violation is reported without failing. Lets rule validation be declared once as a turnkey CI gate instead of passing --rules each run. Overridden by --rules."` //nolint:lll
}

// ScanConfig defines configuration for the workload scan command, letting
// 'ksail workload scan' (no args) act as a turnkey CI gate the same way
// spec.workload.validation configures 'ksail workload validate'.
type ScanConfig struct {
	// Exceptions is the path to a Kubescape exceptions file forwarded to
	// Kubescape's --exceptions. It uses Kubescape's native exceptions format (a
	// JSON array of PostureExceptionPolicy objects) so a repo can suppress
	// justified, runtime-enforced findings (Kyverno admission mutation, Cilium
	// network policies, VPA-managed resources) that a static scan cannot see, and
	// thus gate at complianceThreshold 100. A relative path is resolved against
	// the ksail.yaml directory.
	Exceptions string `json:"exceptions,omitzero" jsonschema_description:"Path to a Kubescape exceptions file forwarded to 'kubescape --exceptions'. Uses Kubescape's native exceptions format (a JSON array of PostureExceptionPolicy objects) to suppress justified, runtime-enforced findings (e.g. Kyverno admission mutation, Cilium network policies, VPA-managed resources) so 'ksail workload scan' can gate at complianceThreshold 100. A relative path resolves against the ksail.yaml directory. Override per-run with --exceptions."` //nolint:lll

	// Frameworks lists the security frameworks to scan against (e.g. nsa, mitre,
	// cis, pss). Defaults to nsa when unset.
	Frameworks []string `json:"frameworks,omitzero" jsonschema_description:"Security frameworks 'ksail workload scan' (no args) scans against (e.g. nsa, mitre, cis, pss). Defaults to nsa when unset. Override per-run with --framework."` //nolint:lll

	// ComplianceThreshold fails the scan when the compliance score is below this
	// whole-percentage value (0-100). Unset (nil) means no threshold gate. The
	// --compliance-threshold flag accepts fractional values; this config field is
	// an integer percentage (the common CI-gate case).
	ComplianceThreshold *int32 `json:"complianceThreshold,omitzero" jsonschema_description:"Minimum Kubescape compliance score (whole percentage, 0-100) required for 'ksail workload scan' (no args) to pass; the scan fails below it. Unset means no threshold gate. Override per-run with --compliance-threshold (which also accepts fractional values)."` //nolint:lll
}

// WatchConfig defines configuration for the workload watch command.
type WatchConfig struct {
	// Hooks are shell commands to run before each apply cycle.
	// Hooks execute sequentially via "sh -c"; if any hook fails, the apply is skipped for that cycle.
	// CLI-only (local shell commands run by `ksail workload watch`); ignored by the operator.
	Hooks []string `json:"hooks,omitzero" jsonschema_description:"Shell commands to run before each apply (e.g. docker build, make generate). Executed sequentially; if any hook fails the apply is skipped. CLI-only; ignored by the operator."` //nolint:lll
}

// FluxConfig holds the Flux-specific bootstrap configuration KSail applies when
// it seeds Flux during cluster bootstrap: the operator/distribution version pins
// and signature verification for the OCIRepository KSail generates. The version
// pins are optional (an empty value means KSail uses its built-in pinned
// version) and configure only the bootstrap seed — once a GitOps repository owns
// the flux-operator HelmRelease or the FluxInstance, that repository becomes the
// steady-state owner and KSail defers.
type FluxConfig struct {
	// OperatorVersion pins the Flux operator Helm chart version KSail installs
	// when it seeds the operator. When empty, KSail uses its built-in pinned
	// version. Ignored once a GitOps repo owns the flux-operator release — KSail
	// never re-installs over a Helm release managed by Flux or ArgoCD.
	OperatorVersion string `json:"operatorVersion,omitzero" jsonschema_description:"Flux operator Helm chart version for the bootstrap seed. Empty uses KSail's pinned version. Ignored once a GitOps repo owns the flux-operator release."` //nolint:lll
	// DistributionVersion pins the FluxInstance spec.distribution.version KSail
	// seeds. When empty, KSail uses its default ("2.x"). A repo-declared
	// FluxInstance's distribution.version takes precedence over this value.
	DistributionVersion string `json:"distributionVersion,omitzero" jsonschema_description:"FluxInstance spec.distribution.version for the bootstrap seed. Empty uses KSail's default (2.x). A repo-declared FluxInstance takes precedence."` //nolint:lll
	// Verify configures cosign/notation signature verification rendered onto the
	// generated flux-system OCIRepository. Empty leaves verification off.
	Verify FluxVerifySpec `json:"verify,omitzero" jsonschema_description:"Signature verification (cosign/notation) rendered onto the flux-system OCIRepository KSail generates, so Flux rejects artifacts whose signature fails verification. Empty disables it."` //nolint:lll
}

// ChatSpec defines AI chat assistant configuration.
type ChatSpec struct {
	Model string `json:"model,omitzero" jsonschema_description:"Chat model (empty or 'auto' for API default)"`
	// ReasoningEffort specifies the reasoning effort level for chat responses.
	// Valid values: "low", "medium", "high"
	ReasoningEffort string `json:"reasoningEffort,omitzero" jsonschema:"enum=low,enum=medium,enum=high" jsonschema_description:"Reasoning effort level for chat responses (low, medium, or high)"` //nolint:lll // Long description required for JSON schema
}

// Connection defines connection options for a KSail cluster.
type Connection struct {
	// Kubeconfig is the path to the kubeconfig file KSail reads and writes.
	// Defaults to "~/.kube/config".
	Kubeconfig string `default:"~/.kube/config" json:"kubeconfig,omitzero"`
	// Context is the kubeconfig context for the cluster. When empty, KSail derives
	// it from the distribution and cluster name (e.g. "kind-kind").
	Context string `json:"context,omitzero"`
	// Timeout is the maximum time KSail waits for cluster operations to complete
	// (e.g. "5m").
	Timeout metav1.Duration `json:"timeout,omitzero"`
}
