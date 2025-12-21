package v1alpha1

import (
	"fmt"
	"slices"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// Group is the API group for KSail.
	Group = "ksail.dev"
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
	Editor   string        `json:"editor,omitzero"   jsonschema:"description=Editor command for interactive workflows (e.g. code --wait)"` //nolint:lll
	Cluster  ClusterSpec   `json:"cluster,omitzero"`
	Workload WorkloadSpec  `json:"workload,omitzero"`
}

// ClusterSpec defines cluster-related configuration.
type ClusterSpec struct {
	DistributionConfig                string        `json:"distributionConfig,omitzero"`
	Connection                        Connection    `json:"connection,omitzero"`
	Distribution                      Distribution  `json:"distribution,omitzero"`
	CNI                               CNI           `json:"cni,omitzero"`
	CSI                               CSI           `json:"csi,omitzero"`
	MetricsServer                     MetricsServer `json:"metricsServer,omitzero"`
	CertManager                       CertManager   `json:"certManager,omitzero"`
	LocalRegistry                     LocalRegistry `json:"localRegistry,omitzero"`
	GitOpsEngine                      GitOpsEngine  `json:"gitOpsEngine,omitzero"`
	PushAndReconcileWorkloadOnCreate  bool          `json:"pushAndReconcileWorkloadOnCreate,omitzero"`
	Options                           Options       `json:"options,omitzero"`
}

// WorkloadSpec defines workload-related configuration.
type WorkloadSpec struct {
	SourceDirectory string `json:"sourceDirectory,omitzero"`
}

// Connection defines connection options for a KSail cluster.
type Connection struct {
	Kubeconfig string          `json:"kubeconfig,omitzero"`
	Context    string          `json:"context,omitzero"`
	Timeout    metav1.Duration `json:"timeout,omitzero"`
}

// --- OCI Registry Types ---

// OCIRegistryStatus represents lifecycle states for the local OCI registry instance.
type OCIRegistryStatus string

const (
	// OCIRegistryStatusNotProvisioned indicates the registry has not been created.
	OCIRegistryStatusNotProvisioned OCIRegistryStatus = "NotProvisioned"
	// OCIRegistryStatusProvisioning indicates the registry is currently being created or started.
	OCIRegistryStatusProvisioning OCIRegistryStatus = "Provisioning"
	// OCIRegistryStatusRunning indicates the registry is available for pushes/pulls.
	OCIRegistryStatusRunning OCIRegistryStatus = "Running"
	// OCIRegistryStatusError indicates the registry failed to start or crashed.
	OCIRegistryStatusError OCIRegistryStatus = "Error"
)

// OCIRegistry captures host-local OCI registry metadata and lifecycle status.
type OCIRegistry struct {
	Name       string            `json:"name,omitzero"`
	Endpoint   string            `json:"endpoint,omitzero"`
	Port       int32             `json:"port,omitzero"`
	DataPath   string            `json:"dataPath,omitzero"`
	VolumeName string            `json:"volumeName,omitzero"`
	Status     OCIRegistryStatus `json:"status,omitzero"`
	LastError  string            `json:"lastError,omitzero"`
}

// OCIArtifact describes a versioned OCI artifact that packages Kubernetes manifests.
type OCIArtifact struct {
	Name             string      `json:"name,omitzero"`
	Version          string      `json:"version,omitzero"`
	RegistryEndpoint string      `json:"registryEndpoint,omitzero"`
	Repository       string      `json:"repository,omitzero"`
	Tag              string      `json:"tag,omitzero"`
	SourcePath       string      `json:"sourcePath,omitzero"`
	CreatedAt        metav1.Time `json:"createdAt,omitzero"`
}

// --- Flux Types ---

// FluxObjectMeta provides the minimal metadata required for Flux custom resources.
type FluxObjectMeta struct {
	Name      string `json:"name,omitzero"`
	Namespace string `json:"namespace,omitzero"`
}

// FluxOCIRepository models the Flux OCIRepository custom resource fields relevant to KSail-Go.
type FluxOCIRepository struct {
	Metadata FluxObjectMeta          `json:"metadata,omitzero"`
	Spec     FluxOCIRepositorySpec   `json:"spec,omitzero"`
	Status   FluxOCIRepositoryStatus `json:"status,omitzero"`
}

// FluxOCIRepositorySpec encodes connection details to an OCI registry repository.
type FluxOCIRepositorySpec struct {
	URL      string               `json:"url,omitzero"`
	Interval metav1.Duration      `json:"interval,omitzero"`
	Ref      FluxOCIRepositoryRef `json:"ref,omitzero"`
}

// FluxOCIRepositoryRef targets a specific OCI artifact tag.
type FluxOCIRepositoryRef struct {
	Tag string `json:"tag,omitzero"`
}

// FluxOCIRepositoryStatus exposes reconciliation conditions for OCIRepository resources.
type FluxOCIRepositoryStatus struct {
	Conditions []metav1.Condition `json:"conditions,omitzero"`
}

// FluxKustomization models the Flux Kustomization custom resource fields relevant to KSail-Go.
type FluxKustomization struct {
	Metadata FluxObjectMeta          `json:"metadata,omitzero"`
	Spec     FluxKustomizationSpec   `json:"spec,omitzero"`
	Status   FluxKustomizationStatus `json:"status,omitzero"`
}

// FluxKustomizationSpec defines how Flux should apply manifests from a referenced source.
type FluxKustomizationSpec struct {
	Path            string                     `json:"path,omitzero"`
	Interval        metav1.Duration            `json:"interval,omitzero"`
	Prune           bool                       `json:"prune,omitzero"`
	TargetNamespace string                     `json:"targetNamespace,omitzero"`
	SourceRef       FluxKustomizationSourceRef `json:"sourceRef,omitzero"`
}

// FluxKustomizationSourceRef identifies the Flux source object backing a Kustomization.
type FluxKustomizationSourceRef struct {
	Name      string `json:"name,omitzero"`
	Namespace string `json:"namespace,omitzero"`
}

// FluxKustomizationStatus exposes reconciliation conditions for Kustomization resources.
type FluxKustomizationStatus struct {
	Conditions []metav1.Condition `json:"conditions,omitzero"`
}

// --- Distribution Types ---

// Distribution defines the distribution options for a KSail cluster.
type Distribution string

const (
	// DistributionKind is the kind distribution.
	DistributionKind Distribution = "Kind"
	// DistributionK3d is the K3d distribution.
	DistributionK3d Distribution = "K3d"
)

// ProvidesMetricsServerByDefault returns true if the distribution includes metrics-server by default.
// K3d (based on K3s) includes metrics-server, Kind does not.
func (d *Distribution) ProvidesMetricsServerByDefault() bool {
	switch *d {
	case DistributionK3d:
		return true
	case DistributionKind:
		return false
	default:
		return false
	}
}

// ProvidesStorageByDefault returns true if the distribution includes a storage provisioner by default.
// K3d (based on K3s) includes local-path-provisioner, Kind does not have a default storage class.
func (d *Distribution) ProvidesStorageByDefault() bool {
	switch *d {
	case DistributionK3d:
		return true
	case DistributionKind:
		return false
	default:
		return false
	}
}

// --- CNI Types ---

// CNI defines the CNI options for a KSail cluster.
type CNI string

const (
	// CNIDefault is the default CNI.
	CNIDefault CNI = "Default"
	// CNICilium is the Cilium CNI.
	CNICilium CNI = "Cilium"
	// CNICalico is the Calico CNI.
	CNICalico CNI = "Calico"
)

// --- CSI Types ---

// CSI defines the CSI options for a KSail cluster.
type CSI string

const (
	// CSIDefault is the default CSI.
	CSIDefault CSI = "Default"
	// CSILocalPathStorage is the LocalPathStorage CSI.
	CSILocalPathStorage CSI = "LocalPathStorage"
)

// --- Metrics Server Types ---

// MetricsServer defines the Metrics Server options for a KSail cluster.
type MetricsServer string

const (
	// MetricsServerEnabled ensures Metrics Server is installed.
	MetricsServerEnabled MetricsServer = "Enabled"
	// MetricsServerDisabled ensures Metrics Server is not installed.
	MetricsServerDisabled MetricsServer = "Disabled"
)

// --- Cert-Manager Types ---

// CertManager defines the cert-manager options for a KSail cluster.
type CertManager string

const (
	// CertManagerEnabled ensures cert-manager is installed.
	CertManagerEnabled CertManager = "Enabled"
	// CertManagerDisabled ensures cert-manager is not installed.
	CertManagerDisabled CertManager = "Disabled"
)

// --- Local Registry Types ---

// LocalRegistry defines how the host-local OCI registry should behave.
type LocalRegistry string

const (
	// LocalRegistryEnabled provisions and manages the local registry lifecycle.
	LocalRegistryEnabled LocalRegistry = "Enabled"
	// LocalRegistryDisabled skips local registry provisioning.
	LocalRegistryDisabled LocalRegistry = "Disabled"
)

// --- GitOps Engine Types ---

// GitOpsEngine defines the GitOps Engine options for a KSail cluster.
type GitOpsEngine string

const (
	// GitOpsEngineNone is the default and disables managed GitOps integration.
	// It means "no GitOps engine" is configured for the cluster.
	GitOpsEngineNone GitOpsEngine = "None"
	// GitOpsEngineFlux installs and manages Flux controllers.
	GitOpsEngineFlux GitOpsEngine = "Flux"
	// GitOpsEngineArgoCD installs and manages Argo CD.
	GitOpsEngineArgoCD GitOpsEngine = "ArgoCD"
)

// --- Options Types ---

// Options holds optional settings for distributions, networking, and deployment tools.
type Options struct {
	Kind OptionsKind `json:"kind,omitzero"`
	K3d  OptionsK3d  `json:"k3d,omitzero"`

	Cilium OptionsCilium `json:"cilium,omitzero"`
	Calico OptionsCalico `json:"calico,omitzero"`

	Flux          OptionsFlux          `json:"flux,omitzero"`
	ArgoCD        OptionsArgoCD        `json:"argocd,omitzero"`
	LocalRegistry OptionsLocalRegistry `json:"localRegistry,omitzero"`

	Helm      OptionsHelm      `json:"helm,omitzero"`
	Kustomize OptionsKustomize `json:"kustomize,omitzero"`
}

// OptionsKind defines options specific to the Kind distribution.
type OptionsKind struct {
	// Add any specific fields for the Kind distribution here.
}

// OptionsK3d defines options specific to the K3d distribution.
type OptionsK3d struct{}

// OptionsCilium defines options for the Cilium CNI.
type OptionsCilium struct {
	// Add any specific fields for the Cilium CNI here.
}

// OptionsCalico defines options for the Calico CNI.
type OptionsCalico struct {
	// Add any specific fields for the Calico CNI here.
}

// OptionsFlux defines options for the Flux deployment tool.
type OptionsFlux struct {
	Interval metav1.Duration `json:"interval,omitzero"`
}

// OptionsArgoCD defines options for the ArgoCD deployment tool.
type OptionsArgoCD struct {
	// Add any specific fields for the ArgoCD tool here.
}

// OptionsLocalRegistry defines options for the host-local OCI registry integration.
type OptionsLocalRegistry struct {
	HostPort int32 `json:"hostPort,omitzero"`
}

// OptionsHelm defines options for the Helm tool.
type OptionsHelm struct {
	// Add any specific fields for the Helm tool here.
}

// OptionsKustomize defines options for the Kustomize tool.
type OptionsKustomize struct {
	// Add any specific fields for the Kustomize tool here.
}

// --- pflags Interface Implementations ---

// Set for Distribution.
func (d *Distribution) Set(value string) error {
	// Check against constant values with case-insensitive comparison
	for _, dist := range validDistributions() {
		if strings.EqualFold(value, string(dist)) {
			*d = dist

			return nil
		}
	}

	return fmt.Errorf("%w: %s (valid options: %s, %s)",
		ErrInvalidDistribution, value, DistributionKind, DistributionK3d)
}

// Set for GitOpsEngine.
func (g *GitOpsEngine) Set(value string) error {
	// Check against constant values with case-insensitive comparison
	for _, tool := range validGitOpsEngines() {
		if strings.EqualFold(value, string(tool)) {
			*g = tool

			return nil
		}
	}

	return fmt.Errorf(
		"%w: %s (valid options: %s, %s, %s)",
		ErrInvalidGitOpsEngine,
		value,
		GitOpsEngineNone,
		GitOpsEngineFlux,
		GitOpsEngineArgoCD,
	)
}

// Set for CNI.
func (c *CNI) Set(value string) error {
	// Check against constant values with case-insensitive comparison
	for _, cni := range validCNIs() {
		if strings.EqualFold(value, string(cni)) {
			*c = cni

			return nil
		}
	}

	return fmt.Errorf("%w: %s (valid options: %s, %s, %s)",
		ErrInvalidCNI, value, CNIDefault, CNICilium, CNICalico)
}

// Set for CSI.
func (c *CSI) Set(value string) error {
	// Check against constant values with case-insensitive comparison
	for _, csi := range validCSIs() {
		if strings.EqualFold(value, string(csi)) {
			*c = csi

			return nil
		}
	}

	return fmt.Errorf("%w: %s (valid options: %s, %s)",
		ErrInvalidCSI, value, CSIDefault, CSILocalPathStorage)
}

// Set for MetricsServer.
func (m *MetricsServer) Set(value string) error {
	// Check against constant values with case-insensitive comparison
	for _, ms := range validMetricsServers() {
		if strings.EqualFold(value, string(ms)) {
			*m = ms

			return nil
		}
	}

	return fmt.Errorf(
		"%w: %s (valid options: %s, %s)",
		ErrInvalidMetricsServer,
		value,
		MetricsServerEnabled,
		MetricsServerDisabled,
	)
}

// Set for CertManager.
func (c *CertManager) Set(value string) error {
	// Check against constant values with case-insensitive comparison
	for _, cm := range validCertManagers() {
		if strings.EqualFold(value, string(cm)) {
			*c = cm

			return nil
		}
	}

	return fmt.Errorf(
		"%w: %s (valid options: %s, %s)",
		ErrInvalidCertManager,
		value,
		CertManagerEnabled,
		CertManagerDisabled,
	)
}

// Set for LocalRegistry.
func (l *LocalRegistry) Set(value string) error {
	// Check against constant values with case-insensitive comparison
	for _, mode := range validLocalRegistryModes() {
		if strings.EqualFold(value, string(mode)) {
			*l = mode

			return nil
		}
	}

	return fmt.Errorf(
		"%w: %s (valid options: %s, %s)",
		ErrInvalidLocalRegistry,
		value,
		LocalRegistryEnabled,
		LocalRegistryDisabled,
	)
}

// String returns the string representation of the LocalRegistry.
func (l *LocalRegistry) String() string {
	return string(*l)
}

// Type returns the type of the LocalRegistry.
func (l *LocalRegistry) Type() string {
	return "LocalRegistry"
}

// IsValid checks if the distribution value is supported.
func (d *Distribution) IsValid() bool {
	return slices.Contains(validDistributions(), *d)
}

// String returns the string representation of the Distribution.
func (d *Distribution) String() string {
	return string(*d)
}

// Type returns the type of the Distribution.
func (d *Distribution) Type() string {
	return "Distribution"
}

// String returns the string representation of the GitOpsEngine.
func (g *GitOpsEngine) String() string {
	return string(*g)
}

// Type returns the type of the GitOpsEngine.
func (g *GitOpsEngine) Type() string {
	return "GitOpsEngine"
}

// String returns the string representation of the CNI.
func (c *CNI) String() string {
	return string(*c)
}

// Type returns the type of the CNI.
func (c *CNI) Type() string {
	return "CNI"
}

// String returns the string representation of the CSI.
func (c *CSI) String() string {
	return string(*c)
}

// Type returns the type of the CSI.
func (c *CSI) Type() string {
	return "CSI"
}

// String returns the string representation of the MetricsServer.
func (m *MetricsServer) String() string {
	return string(*m)
}

// Type returns the type of the MetricsServer.
func (m *MetricsServer) Type() string {
	return "MetricsServer"
}

// String returns the string representation of the CertManager.
func (c *CertManager) String() string {
	return string(*c)
}

// Type returns the type of the CertManager.
func (c *CertManager) Type() string {
	return "CertManager"
}
