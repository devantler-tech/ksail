package tenant

import (
	"fmt"
	"strings"

	"github.com/devantler-tech/ksail/v5/pkg/svc/detector/gitops"
	"sigs.k8s.io/yaml"
)

const (
	appProjectKind   = "AppProject"
	k8sDefaultServer = "https://kubernetes.default.svc"
	rbacConfigMapName = "argocd-rbac-cm"
)

// appProject represents an ArgoCD AppProject CR.
type appProject struct {
	APIVersion string          `json:"apiVersion" yaml:"apiVersion"`
	Kind       string          `json:"kind"       yaml:"kind"`
	Metadata   argoCDMeta      `json:"metadata"   yaml:"metadata"`
	Spec       appProjectSpec  `json:"spec"       yaml:"spec"`
}

type argoCDMeta struct {
	Name      string            `json:"name"             yaml:"name"`
	Namespace string            `json:"namespace"        yaml:"namespace"`
	Labels    map[string]string `json:"labels,omitempty" yaml:"labels,omitempty"`
}

type appProjectSpec struct {
	Description  string                  `json:"description"  yaml:"description"`
	SourceRepos  []string                `json:"sourceRepos"  yaml:"sourceRepos"`
	Destinations []appProjectDestination `json:"destinations" yaml:"destinations"`
}

type appProjectDestination struct {
	Namespace string `json:"namespace" yaml:"namespace"`
	Server    string `json:"server"    yaml:"server"`
}

// argoCDApp represents an ArgoCD Application CR for tenant generation.
type argoCDApp struct {
	APIVersion string        `json:"apiVersion" yaml:"apiVersion"`
	Kind       string        `json:"kind"       yaml:"kind"`
	Metadata   argoCDMeta    `json:"metadata"   yaml:"metadata"`
	Spec       argoCDAppSpec `json:"spec"       yaml:"spec"`
}

type argoCDAppSpec struct {
	Project     string           `json:"project"     yaml:"project"`
	Source      argoCDAppSource  `json:"source"      yaml:"source"`
	Destination argoCDAppDest   `json:"destination"  yaml:"destination"`
	SyncPolicy  argoCDSyncPolicy `json:"syncPolicy"  yaml:"syncPolicy"`
}

type argoCDAppSource struct {
	//nolint:tagliatelle // ArgoCD requires this exact casing
	RepoURL        string `json:"repoURL"        yaml:"repoURL"`
	TargetRevision string `json:"targetRevision"  yaml:"targetRevision"`
	Path           string `json:"path"            yaml:"path"`
}

type argoCDAppDest struct {
	Server    string `json:"server"    yaml:"server"`
	Namespace string `json:"namespace" yaml:"namespace"`
}

type argoCDSyncPolicy struct {
	Automated argoCDAutoSync `json:"automated" yaml:"automated"`
}

type argoCDAutoSync struct {
	Prune    bool `json:"prune"    yaml:"prune"`
	SelfHeal bool `json:"selfHeal" yaml:"selfHeal"`
}

// rbacConfigMap represents a Kubernetes ConfigMap for ArgoCD RBAC.
type rbacConfigMap struct {
	APIVersion string            `json:"apiVersion" yaml:"apiVersion"`
	Kind       string            `json:"kind"       yaml:"kind"`
	Metadata   argoCDMeta        `json:"metadata"   yaml:"metadata"`
	Data       map[string]string `json:"data"       yaml:"data"`
}

const argoCDManifestCount = 2

// GenerateArgoCDManifests generates ArgoCD-specific tenant manifests.
// Returns a map of filename -> YAML content.
// Files: project.yaml, app.yaml
//
// Note: ArgoCD RBAC ConfigMap (argocd-rbac-cm) is NOT generated per-tenant
// to avoid kustomize conflicts when multiple tenants share the same namespace.
// Use MergeArgoCDRBACPolicy to add tenant policies to a shared argocd-rbac-cm.
func GenerateArgoCDManifests(opts Options) (map[string]string, error) {
	if opts.GitProvider == "" {
		return nil, fmt.Errorf("%w for ArgoCD tenants", ErrGitProviderRequired)
	}
	if opts.GitRepo == "" {
		return nil, fmt.Errorf("%w for ArgoCD tenants", ErrGitRepoRequired)
	}
	if len(opts.Namespaces) == 0 {
		return nil, fmt.Errorf("%w", ErrNamespaceRequired)
	}

	result := make(map[string]string, argoCDManifestCount)

	projectYAML, err := generateAppProject(opts)
	if err != nil {
		return nil, fmt.Errorf("generating ArgoCD AppProject: %w", err)
	}
	result["project.yaml"] = projectYAML

	appYAML, err := generateArgoCDApp(opts)
	if err != nil {
		return nil, fmt.Errorf("generating ArgoCD Application: %w", err)
	}
	result["app.yaml"] = appYAML

	return result, nil
}

func generateAppProject(opts Options) (string, error) {
	destinations := make([]appProjectDestination, len(opts.Namespaces))
	for i, ns := range opts.Namespaces {
		destinations[i] = appProjectDestination{
			Namespace: ns,
			Server:    k8sDefaultServer,
		}
	}

	host := gitProviderHost(opts.GitProvider)
	repoURL := fmt.Sprintf("https://%s/%s", host, opts.GitRepo)

	project := appProject{
		APIVersion: gitops.ArgoCDApplicationAPIVersion,
		Kind:       appProjectKind,
		Metadata: argoCDMeta{
			Name:      opts.Name,
			Namespace: gitops.ArgoCDNamespace,
			Labels:    ManagedByLabels(),
		},
		Spec: appProjectSpec{
			Description:  fmt.Sprintf("Tenant project for %s", opts.Name),
			SourceRepos:  []string{repoURL},
			Destinations: destinations,
		},
	}

	data, err := yaml.Marshal(project)
	if err != nil {
		return "", fmt.Errorf("marshaling AppProject: %w", err)
	}

	return string(data), nil
}

func gitProviderHost(provider string) string {
	switch strings.ToLower(provider) {
	case "github":
		return "github.com"
	case "gitlab":
		return "gitlab.com"
	case "gitea":
		return "gitea.com"
	default:
		return provider
	}
}

func generateArgoCDApp(opts Options) (string, error) {
	host := gitProviderHost(opts.GitProvider)
	repoURL := fmt.Sprintf("https://%s/%s", host, opts.GitRepo)
	primaryNS := opts.Namespaces[0]

	app := argoCDApp{
		APIVersion: gitops.ArgoCDApplicationAPIVersion,
		Kind:       gitops.ArgoCDApplicationKind,
		Metadata: argoCDMeta{
			Name:      opts.Name,
			Namespace: gitops.ArgoCDNamespace,
			Labels:    ManagedByLabels(),
		},
		Spec: argoCDAppSpec{
			Project: opts.Name,
			Source: argoCDAppSource{
				RepoURL:        repoURL,
				TargetRevision: "HEAD",
				Path:           "k8s",
			},
			Destination: argoCDAppDest{
				Server:    k8sDefaultServer,
				Namespace: primaryNS,
			},
			SyncPolicy: argoCDSyncPolicy{
				Automated: argoCDAutoSync{
					Prune:    true,
					SelfHeal: true,
				},
			},
		},
	}

	data, err := yaml.Marshal(app)
	if err != nil {
		return "", fmt.Errorf("marshaling Application: %w", err)
	}

	return string(data), nil
}

func buildTenantPolicyCSV(tenantName string) string {
	lines := []string{
		fmt.Sprintf("p, role:%s, applications, *, %s/*, allow", tenantName, tenantName),
		fmt.Sprintf("p, role:%s, projects, get, %s, allow", tenantName, tenantName),
		fmt.Sprintf("g, %s, role:%s", tenantName, tenantName),
	}
	return strings.Join(lines, "\n") + "\n"
}

func newRBACConfigMap(policyCSV string) rbacConfigMap {
	return rbacConfigMap{
		APIVersion: "v1",
		Kind:       "ConfigMap",
		Metadata: argoCDMeta{
			Name:      rbacConfigMapName,
			Namespace: gitops.ArgoCDNamespace,
			Labels:    ManagedByLabels(),
		},
		Data: map[string]string{
			"policy.csv": policyCSV,
		},
	}
}

// MergeArgoCDRBACPolicy intelligently merges tenant policies into existing argocd-rbac-cm content.
// If existingContent is empty, creates a new ConfigMap.
func MergeArgoCDRBACPolicy(existingContent string, tenantName string) (string, error) {
	if strings.TrimSpace(existingContent) == "" {
		cm := newRBACConfigMap(buildTenantPolicyCSV(tenantName))
		data, err := yaml.Marshal(cm)
		if err != nil {
			return "", fmt.Errorf("marshaling RBAC ConfigMap: %w", err)
		}
		return string(data), nil
	}

	var configMap rbacConfigMap
	err := yaml.Unmarshal([]byte(existingContent), &configMap)
	if err != nil {
		return "", fmt.Errorf("parsing existing RBAC ConfigMap: %w", err)
	}

	if configMap.Data == nil {
		configMap.Data = make(map[string]string)
	}

	existingPolicy := configMap.Data["policy.csv"]

	// Use exact role boundary matching to avoid false positives when
	// one tenant name is a substring of another (e.g., "team" vs "team-alpha").
	if hasTenantPolicy(existingPolicy, tenantName) {
		data, err := yaml.Marshal(configMap)
		if err != nil {
			return "", fmt.Errorf("marshaling RBAC ConfigMap: %w", err)
		}
		return string(data), nil
	}

	tenantPolicy := buildTenantPolicyCSV(tenantName)
	if existingPolicy != "" && !strings.HasSuffix(existingPolicy, "\n") {
		existingPolicy += "\n"
	}
	configMap.Data["policy.csv"] = existingPolicy + tenantPolicy

	data, err := yaml.Marshal(configMap)
	if err != nil {
		return "", fmt.Errorf("marshaling RBAC ConfigMap: %w", err)
	}

	return string(data), nil
}

// RemoveArgoCDRBACPolicy removes a tenant's policy lines from argocd-rbac-cm content.
// Used by the delete command.
func RemoveArgoCDRBACPolicy(existingContent string, tenantName string) (string, error) {
	var configMap rbacConfigMap
	err := yaml.Unmarshal([]byte(existingContent), &configMap)
	if err != nil {
		return "", fmt.Errorf("parsing existing RBAC ConfigMap: %w", err)
	}

	if configMap.Data == nil {
		data, err := yaml.Marshal(configMap)
		if err != nil {
			return "", fmt.Errorf("marshaling RBAC ConfigMap: %w", err)
		}
		return string(data), nil
	}

	existingPolicy := configMap.Data["policy.csv"]

	lines := strings.Split(existingPolicy, "\n")
	filtered := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if isTenantPolicyLine(trimmed, tenantName) {
			continue
		}
		filtered = append(filtered, line)
	}

	result := strings.TrimRight(strings.Join(filtered, "\n"), "\n")
	if result != "" {
		result += "\n"
	}
	configMap.Data["policy.csv"] = result

	data, err := yaml.Marshal(configMap)
	if err != nil {
		return "", fmt.Errorf("marshaling RBAC ConfigMap: %w", err)
	}

	return string(data), nil
}

// hasTenantPolicy checks if the given policy CSV already contains policies
// for the exact tenant name, avoiding substring false positives.
func hasTenantPolicy(policyCSV, tenantName string) bool {
	for _, line := range strings.Split(policyCSV, "\n") {
		if isTenantPolicyLine(strings.TrimSpace(line), tenantName) {
			return true
		}
	}
	return false
}

// isTenantPolicyLine checks if a single policy line belongs to the given tenant,
// using exact field matching to avoid substring collisions.
func isTenantPolicyLine(line, tenantName string) bool {
	if line == "" {
		return false
	}
	exactRole := fmt.Sprintf("role:%s,", tenantName)
	exactGroup := fmt.Sprintf("g, %s, role:%s", tenantName, tenantName)
	return strings.Contains(line, exactRole) || strings.TrimSpace(line) == exactGroup
}
