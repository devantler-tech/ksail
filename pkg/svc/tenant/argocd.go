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

// GenerateArgoCDManifests generates ArgoCD-specific tenant manifests.
// Returns a map of filename -> YAML content.
// Files: project.yaml, app.yaml, argocd-rbac-cm.yaml
func GenerateArgoCDManifests(opts Options) (map[string]string, error) {
	result := make(map[string]string, 3)

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

	rbacYAML, err := generateRBACConfigMap(opts)
	if err != nil {
		return nil, fmt.Errorf("generating ArgoCD RBAC ConfigMap: %w", err)
	}
	result["argocd-rbac-cm.yaml"] = rbacYAML

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
			SourceRepos:  []string{"*"},
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

func generateRBACConfigMap(opts Options) (string, error) {
	cm := newRBACConfigMap(buildTenantPolicyCSV(opts.Name))

	data, err := yaml.Marshal(cm)
	if err != nil {
		return "", fmt.Errorf("marshaling RBAC ConfigMap: %w", err)
	}

	return string(data), nil
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

	var cm rbacConfigMap
	if err := yaml.Unmarshal([]byte(existingContent), &cm); err != nil {
		return "", fmt.Errorf("parsing existing RBAC ConfigMap: %w", err)
	}

	if cm.Data == nil {
		cm.Data = make(map[string]string)
	}

	existingPolicy := cm.Data["policy.csv"]
	roleKey := fmt.Sprintf("role:%s", tenantName)

	if strings.Contains(existingPolicy, roleKey) {
		data, err := yaml.Marshal(cm)
		if err != nil {
			return "", fmt.Errorf("marshaling RBAC ConfigMap: %w", err)
		}
		return string(data), nil
	}

	tenantPolicy := buildTenantPolicyCSV(tenantName)
	if existingPolicy != "" && !strings.HasSuffix(existingPolicy, "\n") {
		existingPolicy += "\n"
	}
	cm.Data["policy.csv"] = existingPolicy + tenantPolicy

	data, err := yaml.Marshal(cm)
	if err != nil {
		return "", fmt.Errorf("marshaling RBAC ConfigMap: %w", err)
	}

	return string(data), nil
}

// RemoveArgoCDRBACPolicy removes a tenant's policy lines from argocd-rbac-cm content.
// Used by the delete command.
func RemoveArgoCDRBACPolicy(existingContent string, tenantName string) (string, error) {
	var cm rbacConfigMap
	if err := yaml.Unmarshal([]byte(existingContent), &cm); err != nil {
		return "", fmt.Errorf("parsing existing RBAC ConfigMap: %w", err)
	}

	if cm.Data == nil {
		data, err := yaml.Marshal(cm)
		if err != nil {
			return "", fmt.Errorf("marshaling RBAC ConfigMap: %w", err)
		}
		return string(data), nil
	}

	existingPolicy := cm.Data["policy.csv"]
	roleKey := fmt.Sprintf("role:%s", tenantName)
	groupPrefix := fmt.Sprintf("g, %s,", tenantName)

	lines := strings.Split(existingPolicy, "\n")
	filtered := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, roleKey) || strings.HasPrefix(trimmed, groupPrefix) {
			continue
		}
		filtered = append(filtered, line)
	}

	result := strings.TrimRight(strings.Join(filtered, "\n"), "\n")
	if result != "" {
		result += "\n"
	}
	cm.Data["policy.csv"] = result

	data, err := yaml.Marshal(cm)
	if err != nil {
		return "", fmt.Errorf("marshaling RBAC ConfigMap: %w", err)
	}

	return string(data), nil
}
