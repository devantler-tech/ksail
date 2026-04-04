package tenant

import (
	"fmt"
	"strings"
)

// TenantType defines the type of tenant (determines which resources are generated).
type TenantType string

const (
	// TenantTypeFlux generates RBAC + Flux sync manifests (OCIRepository/GitRepository + Kustomization).
	TenantTypeFlux TenantType = "flux"
	// TenantTypeArgoCD generates RBAC + ArgoCD manifests (AppProject + Application + argocd-rbac-cm).
	TenantTypeArgoCD TenantType = "argocd"
	// TenantTypeKubectl generates RBAC manifests only (no GitOps sync resources).
	TenantTypeKubectl TenantType = "kubectl"
)

// ValidTenantTypes returns all valid tenant type values.
func ValidTenantTypes() []TenantType {
	return []TenantType{TenantTypeFlux, TenantTypeArgoCD, TenantTypeKubectl}
}

// Set implements pflag.Value for TenantType (case-insensitive).
func (t *TenantType) Set(value string) error {
	for _, valid := range ValidTenantTypes() {
		if strings.EqualFold(value, string(valid)) {
			*t = valid
			return nil
		}
	}
	return fmt.Errorf("%w: %s (valid options: %s)", ErrInvalidTenantType, value, strings.Join(validTenantTypeStrings(), ", "))
}

// String returns the string representation of the TenantType.
func (t *TenantType) String() string {
	return string(*t)
}

// Type returns the type name for pflag.
func (t *TenantType) Type() string {
	return "TenantType"
}

func validTenantTypeStrings() []string {
	types := ValidTenantTypes()
	result := make([]string, len(types))
	for i, t := range types {
		result[i] = string(t)
	}
	return result
}

// SyncSource defines the source type for Flux tenants.
type SyncSource string

const (
	// SyncSourceOCI uses an OCIRepository source for Flux sync.
	SyncSourceOCI SyncSource = "oci"
	// SyncSourceGit uses a GitRepository source for Flux sync.
	SyncSourceGit SyncSource = "git"
)

// Options holds all configuration for tenant generation.
type Options struct {
	// Name is the tenant name (required).
	Name string
	// Namespaces to create (default: [Name]).
	Namespaces []string
	// ClusterRole to bind to the tenant ServiceAccount (default: "edit").
	ClusterRole string
	// OutputDir is the output directory for platform manifests (default: ".").
	OutputDir string
	// Force overwrites existing tenant directory.
	Force bool
	// TenantType is the tenant type: flux, argocd, or kubectl.
	TenantType TenantType
	// SyncSource is the Flux source type: oci or git (Flux only).
	SyncSource SyncSource
	// Registry is the OCI registry URL for Flux OCI source.
	Registry string
	// GitProvider is the Git provider name (github, gitlab, gitea).
	GitProvider string
	// GitRepo is the tenant repo as owner/repo-name.
	GitRepo string
	// GitToken is the Git provider API token.
	GitToken string
	// RepoVisibility is the repo visibility: Private, Internal, or Public.
	RepoVisibility string
	// Register indicates whether to register the tenant in kustomization.yaml.
	Register bool
	// KustomizationPath is the explicit path to kustomization.yaml.
	KustomizationPath string
}

const (
	// DefaultClusterRole is the default ClusterRole bound to tenant ServiceAccounts.
	DefaultClusterRole = "edit"
	// DefaultOutputDir is the default output directory.
	DefaultOutputDir = "."
	// DefaultSyncSource is the default sync source for Flux tenants.
	DefaultSyncSource = SyncSourceOCI
	// DefaultRepoVisibility is the default repository visibility.
	DefaultRepoVisibility = "Private"
)

// ResolveDefaults fills in default values for unset fields.
func (o *Options) ResolveDefaults() {
	if len(o.Namespaces) == 0 {
		o.Namespaces = []string{o.Name}
	}
	if o.ClusterRole == "" {
		o.ClusterRole = DefaultClusterRole
	}
	if o.OutputDir == "" {
		o.OutputDir = DefaultOutputDir
	}
	if o.SyncSource == "" {
		o.SyncSource = DefaultSyncSource
	}
	if o.RepoVisibility == "" {
		o.RepoVisibility = DefaultRepoVisibility
	}
}

// Validate checks that required fields are set.
func (o *Options) Validate() error {
	if o.Name == "" {
		return ErrTenantNameRequired
	}
	if o.TenantType == "" {
		return ErrTenantTypeRequired
	}
	return nil
}

// ManagedByLabels returns the standard KSail managed-by labels.
func ManagedByLabels() map[string]string {
	return map[string]string{
		"app.kubernetes.io/managed-by": "ksail",
	}
}
