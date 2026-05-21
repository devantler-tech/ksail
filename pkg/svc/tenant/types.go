package tenant

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/validation"
)

// Type defines the type of tenant (determines which resources are generated).
type Type string

const (
	// TypeFlux generates RBAC + Flux sync manifests (OCIRepository/GitRepository + Kustomization).
	TypeFlux Type = "flux"
	// TypeArgoCD generates RBAC + ArgoCD manifests (AppProject + Application).
	TypeArgoCD Type = "argocd"
	// TypeKubectl generates RBAC manifests only (no GitOps sync resources).
	TypeKubectl Type = "kubectl"
)

// ValidTypes returns all valid tenant type values.
func ValidTypes() []Type {
	return []Type{TypeFlux, TypeArgoCD, TypeKubectl}
}

// Set implements pflag.Value for Type (case-insensitive).
func (t *Type) Set(value string) error {
	for _, valid := range ValidTypes() {
		if strings.EqualFold(value, string(valid)) {
			*t = valid

			return nil
		}
	}

	return fmt.Errorf(
		"%w: %s (valid options: %s)", ErrInvalidType, value, strings.Join(validTypeStrings(), ", "),
	)
}

// String returns the string representation of the Type.
func (t *Type) String() string {
	if t == nil {
		return ""
	}

	return string(*t)
}

// Type returns the type name for pflag.
func (t *Type) Type() string {
	return "TenantType"
}

func validTypeStrings() []string {
	types := ValidTypes()

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
	TenantType Type
	// SyncSource is the Flux source type: oci or git (Flux only).
	SyncSource SyncSource
	// Registry is the OCI registry URL for Flux OCI source.
	Registry string
	// OCIPath is an optional path suffix appended to the OCIRepository URL
	// to avoid tag collisions when a tenant repo publishes both Docker images
	// and K8s manifests as OCI artifacts (e.g., "deploy" produces oci://registry/owner/repo/deploy).
	OCIPath string
	// GitProvider is the Git provider name (github, gitlab, gitea).
	GitProvider string
	// TenantRepo is the tenant repo as owner/repo-name.
	TenantRepo string
	// GitToken is the Git provider API token.
	GitToken string
	// RepoVisibility is the repo visibility: Private, Internal, or Public.
	RepoVisibility string
	// Register indicates whether to register the tenant in kustomization.yaml.
	Register bool
	// KustomizationPath is the explicit path to kustomization.yaml.
	KustomizationPath string
	// PlatformRepo is the platform repo as owner/repo-name for PR delivery.
	PlatformRepo string
	// TargetBranch is the PR target branch (empty = repo's default branch).
	TargetBranch string
	// SourceDirectory is the directory name for tenant manifests (default: "k8s").
	SourceDirectory string

	// --- Production hardening (all opt-in) ---

	// ClusterRoles are the ClusterRoles to bind to the tenant ServiceAccount.
	// When empty, falls back to ClusterRole (or DefaultClusterRole).
	ClusterRoles []string
	// PodSecurity is the Pod Security Standards level applied to namespaces
	// ("" | restricted | baseline | privileged).
	PodSecurity string
	// DisableTokenAutomount sets automountServiceAccountToken: false on the SA.
	DisableTokenAutomount bool
	// ImagePullSecrets are imagePullSecrets added to the tenant ServiceAccount.
	ImagePullSecrets []string
	// WithNetworkPolicy generates default-deny NetworkPolicies (plus DNS + intra-ns allow).
	WithNetworkPolicy bool
	// NetworkPolicyEngine selects the NetworkPolicy flavor: native or cilium.
	NetworkPolicyEngine NetworkPolicyEngine
	// WithQuota generates a ResourceQuota per namespace.
	WithQuota bool
	// QuotaCPU is the cpu quota (sets requests.cpu and limits.cpu).
	QuotaCPU string
	// QuotaMemory is the memory quota (sets requests.memory and limits.memory).
	QuotaMemory string
	// WithLimitRange generates a LimitRange per namespace.
	WithLimitRange bool
	// LimitDefaultCPU is the default container CPU limit.
	LimitDefaultCPU string
	// LimitDefaultMemory is the default container memory limit.
	LimitDefaultMemory string
	// LimitRequestCPU is the default container CPU request.
	LimitRequestCPU string
	// LimitRequestMemory is the default container memory request.
	LimitRequestMemory string
	// FluxWait sets wait: true (and timeout) on the Flux Kustomization.
	FluxWait bool
	// FluxTimeout is the Flux Kustomization timeout (implies wait).
	FluxTimeout string
	// FluxRetryInterval is the Flux Kustomization retryInterval.
	FluxRetryInterval string
	// FluxDecryption adds a SOPS decryption block to the Flux Kustomization.
	FluxDecryption bool
	// PolicyEngine mirrors the cluster's policy engine (e.g. "Kyverno"), informational.
	PolicyEngine string
}

// NetworkPolicyEngine selects which NetworkPolicy flavor to emit.
type NetworkPolicyEngine string

const (
	// NetworkPolicyEngineNative emits upstream networking.k8s.io/v1 NetworkPolicies.
	NetworkPolicyEngineNative NetworkPolicyEngine = "native"
	// NetworkPolicyEngineCilium emits cilium.io/v2 CiliumNetworkPolicies.
	NetworkPolicyEngineCilium NetworkPolicyEngine = "cilium"
)

const (
	// PodSecurityRestricted is the most restrictive Pod Security Standards level.
	PodSecurityRestricted = "restricted"
	// PodSecurityBaseline is the baseline Pod Security Standards level.
	PodSecurityBaseline = "baseline"
	// PodSecurityPrivileged is the least restrictive Pod Security Standards level.
	PodSecurityPrivileged = "privileged"
)

// ValidPodSecurityLevels returns the valid Pod Security Standards levels.
func ValidPodSecurityLevels() []string {
	return []string{PodSecurityRestricted, PodSecurityBaseline, PodSecurityPrivileged}
}

const (
	// DefaultClusterRole is the default ClusterRole bound to tenant ServiceAccounts.
	DefaultClusterRole = "edit"
	// DefaultOutputDir is the default output directory.
	DefaultOutputDir = "."
	// DefaultSourceDirectory is the default manifest directory name for tenants.
	DefaultSourceDirectory = "k8s"
	// DefaultSyncSource is the default sync source for Flux tenants.
	DefaultSyncSource = SyncSourceOCI
	// DefaultRepoVisibility is the default repository visibility.
	DefaultRepoVisibility = "Private"
	// DefaultQuotaCPU is the default cpu quota.
	DefaultQuotaCPU = "4"
	// DefaultQuotaMemory is the default memory quota.
	DefaultQuotaMemory = "8Gi"
	// DefaultLimitDefaultCPU is the default container CPU limit.
	DefaultLimitDefaultCPU = "500m"
	// DefaultLimitDefaultMemory is the default container memory limit.
	DefaultLimitDefaultMemory = "512Mi"
	// DefaultLimitRequestCPU is the default container CPU request.
	DefaultLimitRequestCPU = "100m"
	// DefaultLimitRequestMemory is the default container memory request.
	DefaultLimitRequestMemory = "128Mi"
	// DefaultFluxTimeout is the default Flux Kustomization timeout when waiting.
	DefaultFluxTimeout = "5m"
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

	if o.SourceDirectory == "" {
		o.SourceDirectory = DefaultSourceDirectory
	}

	o.resolveProductionDefaults()
}

// Validate checks that required fields are set and values are safe.
func (o *Options) Validate() error {
	err := validateTenantName(o.Name)
	if err != nil {
		return err
	}

	if o.TenantType == "" {
		return ErrTenantTypeRequired
	}

	if !isValidType(o.TenantType) {
		return fmt.Errorf("%w: %q", ErrInvalidType, o.TenantType)
	}

	for _, ns := range o.Namespaces {
		if errs := validation.IsDNS1123Label(ns); len(errs) > 0 {
			return fmt.Errorf("%w: namespace %q (%s)",
				ErrInvalidNamespace, ns, strings.Join(errs, "; "))
		}
	}

	if hasDuplicateNamespaces(o.Namespaces) {
		return fmt.Errorf("%w", ErrDuplicateNamespace)
	}

	if o.SourceDirectory != "" {
		err = validateSourceDirectory(o.SourceDirectory)
		if err != nil {
			return err
		}
	}

	return o.validateProduction()
}

// resolveProductionDefaults fills in defaults for the production hardening fields.
func (o *Options) resolveProductionDefaults() {
	if len(o.ClusterRoles) == 0 {
		if o.ClusterRole != "" {
			o.ClusterRoles = []string{o.ClusterRole}
		} else {
			o.ClusterRoles = []string{DefaultClusterRole}
		}
	}

	if o.NetworkPolicyEngine == "" {
		o.NetworkPolicyEngine = NetworkPolicyEngineNative
	}

	if o.WithQuota {
		setDefault(&o.QuotaCPU, DefaultQuotaCPU)
		setDefault(&o.QuotaMemory, DefaultQuotaMemory)
	}

	if o.WithLimitRange {
		setDefault(&o.LimitDefaultCPU, DefaultLimitDefaultCPU)
		setDefault(&o.LimitDefaultMemory, DefaultLimitDefaultMemory)
		setDefault(&o.LimitRequestCPU, DefaultLimitRequestCPU)
		setDefault(&o.LimitRequestMemory, DefaultLimitRequestMemory)
	}

	if o.FluxWait && o.FluxTimeout == "" {
		o.FluxTimeout = DefaultFluxTimeout
	}
}

// validateProduction validates the production hardening fields.
func (o *Options) validateProduction() error {
	err := o.validatePodSecurity()
	if err != nil {
		return err
	}

	err = o.validateClusterRoles()
	if err != nil {
		return err
	}

	err = o.validateResourceQuantities()
	if err != nil {
		return err
	}

	return o.validateDurations()
}

func (o *Options) validatePodSecurity() error {
	if o.PodSecurity != "" && !slices.Contains(ValidPodSecurityLevels(), o.PodSecurity) {
		return fmt.Errorf("%w: %q (valid options: %s)",
			ErrInvalidPodSecurityLevel, o.PodSecurity,
			strings.Join(ValidPodSecurityLevels(), ", "))
	}

	return nil
}

func (o *Options) validateClusterRoles() error {
	for _, role := range o.ClusterRoles {
		if strings.TrimSpace(role) == "" {
			return ErrEmptyClusterRole
		}
	}

	return nil
}

func (o *Options) validateResourceQuantities() error {
	var values []string

	if o.WithQuota {
		values = append(values, o.QuotaCPU, o.QuotaMemory)
	}

	if o.WithLimitRange {
		values = append(values,
			o.LimitDefaultCPU, o.LimitDefaultMemory,
			o.LimitRequestCPU, o.LimitRequestMemory)
	}

	return validateQuantities(values...)
}

func (o *Options) validateDurations() error {
	for _, dur := range []string{o.FluxTimeout, o.FluxRetryInterval} {
		if dur == "" {
			continue
		}

		_, err := time.ParseDuration(dur)
		if err != nil {
			return fmt.Errorf("%w: %q", ErrInvalidDuration, dur)
		}
	}

	return nil
}

func setDefault(field *string, value string) {
	if *field == "" {
		*field = value
	}
}

func validateQuantity(value string) error {
	if value == "" {
		return nil
	}

	_, err := resource.ParseQuantity(value)
	if err != nil {
		return fmt.Errorf("%w: %q", ErrInvalidQuantity, value)
	}

	return nil
}

func validateTenantName(name string) error {
	if name == "" {
		return ErrTenantNameRequired
	}

	if errs := validation.IsDNS1123Label(name); len(errs) > 0 {
		return fmt.Errorf("%w: %s (%s)", ErrInvalidTenantName, name, strings.Join(errs, "; "))
	}

	if strings.Contains(name, "..") || strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("%w: %s (must not contain path separators or '..')",
			ErrInvalidTenantName, name)
	}

	return nil
}

func validateSourceDirectory(dir string) error {
	if strings.Contains(dir, "..") || strings.ContainsAny(dir, `/\`) {
		return fmt.Errorf("%w: %s (must not contain path separators or '..')",
			ErrInvalidSourceDirectory, dir)
	}

	return nil
}

func hasDuplicateNamespaces(namespaces []string) bool {
	seen := make(map[string]bool, len(namespaces))
	for _, ns := range namespaces {
		if seen[ns] {
			return true
		}

		seen[ns] = true
	}

	return false
}

func isValidType(t Type) bool {
	return slices.Contains(ValidTypes(), t)
}

// ManagedByLabels returns the standard KSail managed-by labels.
func ManagedByLabels() map[string]string {
	return map[string]string{
		"app.kubernetes.io/managed-by": "ksail",
	}
}
