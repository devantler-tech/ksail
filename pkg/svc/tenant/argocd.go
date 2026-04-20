package tenant

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	"github.com/devantler-tech/ksail/v7/pkg/svc/detector/gitops"
	"github.com/devantler-tech/ksail/v7/pkg/svc/tenant/gitprovider"
	"sigs.k8s.io/yaml"
)

const (
	appProjectKind    = "AppProject"
	k8sDefaultServer  = "https://kubernetes.default.svc"
	rbacConfigMapName = "argocd-rbac-cm"

	// DefaultArgoCDRBACCMFilename is the default filename when creating a new argocd-rbac-cm file.
	DefaultArgoCDRBACCMFilename = "argocd-rbac-cm.yaml"

	rbacCMFilePermissions = 0o600
)

// appProject represents an ArgoCD AppProject CR.
type appProject struct {
	APIVersion string         `json:"apiVersion" yaml:"apiVersion"`
	Kind       string         `json:"kind"       yaml:"kind"`
	Metadata   argoCDMeta     `json:"metadata"   yaml:"metadata"`
	Spec       appProjectSpec `json:"spec"       yaml:"spec"`
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
	Destination argoCDAppDest    `json:"destination" yaml:"destination"`
	SyncPolicy  argoCDSyncPolicy `json:"syncPolicy"  yaml:"syncPolicy"`
}

type argoCDAppSource struct {
	//nolint:tagliatelle // ArgoCD requires this exact casing
	RepoURL        string `json:"repoURL"        yaml:"repoURL"`
	TargetRevision string `json:"targetRevision" yaml:"targetRevision"`
	Path           string `json:"path"           yaml:"path"`
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

	if opts.TenantRepo == "" {
		return nil, fmt.Errorf("%w for ArgoCD tenants", ErrTenantRepoRequired)
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
	repoURL := fmt.Sprintf("https://%s/%s", host, opts.TenantRepo)

	project := appProject{
		APIVersion: gitops.ArgoCDApplicationAPIVersion,
		Kind:       appProjectKind,
		Metadata: argoCDMeta{
			Name:      opts.Name,
			Namespace: gitops.ArgoCDNamespace,
			Labels:    ManagedByLabels(),
		},
		Spec: appProjectSpec{
			Description:  "Tenant project for " + opts.Name,
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

// gitProviderHost delegates to the shared gitprovider helper.
func gitProviderHost(provider string) string {
	return gitprovider.ResolveProviderHost(provider)
}

func generateArgoCDApp(opts Options) (string, error) {
	host := gitProviderHost(opts.GitProvider)
	repoURL := fmt.Sprintf("https://%s/%s", host, opts.TenantRepo)
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
// Uses map[string]any for round-trip fidelity to preserve unknown fields.
func MergeArgoCDRBACPolicy(existingContent string, tenantName string) (string, error) {
	if strings.TrimSpace(existingContent) == "" {
		cm := newRBACConfigMap(buildTenantPolicyCSV(tenantName))

		data, err := yaml.Marshal(cm)
		if err != nil {
			return "", fmt.Errorf("marshaling RBAC ConfigMap: %w", err)
		}

		return string(data), nil
	}

	raw, err := parseRBACConfigMap(existingContent)
	if err != nil {
		return "", err
	}

	dataMap := ensureDataMap(raw)
	existingPolicy, _ := dataMap["policy.csv"].(string)

	if hasTenantPolicy(existingPolicy, tenantName) {
		return marshalRawConfigMap(raw)
	}

	tenantPolicy := buildTenantPolicyCSV(tenantName)

	if existingPolicy != "" && !strings.HasSuffix(existingPolicy, "\n") {
		existingPolicy += "\n"
	}

	dataMap["policy.csv"] = existingPolicy + tenantPolicy

	return marshalRawConfigMap(raw)
}

// RemoveArgoCDRBACPolicy removes a tenant's policy lines from argocd-rbac-cm content.
// Uses map[string]any for round-trip fidelity to preserve unknown fields.
func RemoveArgoCDRBACPolicy(existingContent string, tenantName string) (string, error) {
	raw, err := parseRBACConfigMap(existingContent)
	if err != nil {
		return "", err
	}

	dataMap := ensureDataMap(raw)

	existingPolicy, _ := dataMap["policy.csv"].(string)
	if existingPolicy == "" {
		return marshalRawConfigMap(raw)
	}

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

	dataMap["policy.csv"] = result

	return marshalRawConfigMap(raw)
}

func parseRBACConfigMap(content string) (map[string]any, error) {
	var raw map[string]any

	err := yaml.Unmarshal([]byte(content), &raw)
	if err != nil {
		return nil, fmt.Errorf("parsing existing RBAC ConfigMap: %w", err)
	}

	return raw, nil
}

func marshalRawConfigMap(raw map[string]any) (string, error) {
	data, err := yaml.Marshal(raw)
	if err != nil {
		return "", fmt.Errorf("marshaling RBAC ConfigMap: %w", err)
	}

	return string(data), nil
}

// ensureDataMap extracts or initializes the "data" sub-map from a raw ConfigMap.
func ensureDataMap(raw map[string]any) map[string]any {
	dataVal, exists := raw["data"]
	if !exists {
		dataMap := make(map[string]any)
		raw["data"] = dataMap

		return dataMap
	}

	dataMap, isMap := dataVal.(map[string]any)
	if !isMap {
		dataMap = make(map[string]any)
		raw["data"] = dataMap

		return dataMap
	}

	return dataMap
}

// hasTenantPolicy checks if the given policy CSV already contains policies
// for the exact tenant name, avoiding substring false positives.
func hasTenantPolicy(policyCSV, tenantName string) bool {
	for line := range strings.SplitSeq(policyCSV, "\n") {
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

// FindArgoCDRBACCM scans YAML files in the given directory for a ConfigMap
// named "argocd-rbac-cm" (apiVersion: v1, kind: ConfigMap).
// Returns the file path if found, or empty string if not found.
// Supports multi-document YAML files separated by "---".
func FindArgoCDRBACCM(dir string) (string, error) {
	canonDir, err := fsutil.EvalCanonicalPath(dir)
	if err != nil {
		return "", fmt.Errorf("resolving directory %s: %w", dir, err)
	}

	entries, err := os.ReadDir(canonDir)
	if err != nil {
		return "", fmt.Errorf("reading directory %s: %w", canonDir, err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
			continue
		}

		filePath := filepath.Join(canonDir, name)

		data, readErr := fsutil.ReadFileSafe(canonDir, filePath)
		if readErr != nil {
			return "", fmt.Errorf("reading %s: %w", filePath, readErr)
		}

		if containsArgoCDRBACCM(data) {
			return filePath, nil
		}
	}

	return "", nil
}

// containsArgoCDRBACCM checks whether YAML data (possibly multi-document)
// contains an argocd-rbac-cm ConfigMap.
func containsArgoCDRBACCM(data []byte) bool {
	content := string(data)
	if strings.HasPrefix(content, "---") {
		content = "\n" + content
	}

	for part := range strings.SplitSeq(content, "\n---") {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}

		var raw map[string]any

		err := yaml.Unmarshal([]byte(trimmed), &raw)
		if err != nil {
			continue
		}

		if isArgoCDRBACConfigMap(raw) {
			return true
		}
	}

	return false
}

func isArgoCDRBACConfigMap(raw map[string]any) bool {
	apiVersion, _ := raw["apiVersion"].(string)
	kind, _ := raw["kind"].(string)

	meta, _ := raw["metadata"].(map[string]any)
	name, _ := meta["name"].(string)

	return apiVersion == "v1" && kind == "ConfigMap" && name == rbacConfigMapName
}

// MergeArgoCDRBACPolicyFile reads an existing argocd-rbac-cm file (or creates a new one)
// and merges the tenant's RBAC policy into it.
func MergeArgoCDRBACPolicyFile(rbacCMPath, tenantName string) error {
	canonPath, err := fsutil.EvalCanonicalPath(rbacCMPath)
	if err != nil {
		return fmt.Errorf("resolving canonical path for %s: %w", rbacCMPath, err)
	}

	existingContent := ""

	data, readErr := os.ReadFile(canonPath) //nolint:gosec // canonicalized above
	if readErr == nil {
		existingContent = string(data)
	} else if !os.IsNotExist(readErr) {
		return fmt.Errorf("reading %s: %w", canonPath, readErr)
	}

	merged, err := MergeArgoCDRBACPolicy(existingContent, tenantName)
	if err != nil {
		return fmt.Errorf("merging RBAC policy: %w", err)
	}

	return writeRBACCMFile(canonPath, merged)
}

// RemoveArgoCDRBACPolicyFile reads an existing argocd-rbac-cm file and removes
// the tenant's RBAC policy from it. No-op if the file does not exist.
func RemoveArgoCDRBACPolicyFile(rbacCMPath, tenantName string) error {
	canonPath, err := fsutil.EvalCanonicalPath(rbacCMPath)
	if err != nil {
		return fmt.Errorf("resolving canonical path for %s: %w", rbacCMPath, err)
	}

	data, readErr := os.ReadFile(canonPath) //nolint:gosec // canonicalized above
	if os.IsNotExist(readErr) {
		return nil
	}

	if readErr != nil {
		return fmt.Errorf("reading %s: %w", canonPath, readErr)
	}

	result, err := RemoveArgoCDRBACPolicy(string(data), tenantName)
	if err != nil {
		return fmt.Errorf("removing RBAC policy: %w", err)
	}

	return writeRBACCMFile(canonPath, result)
}

// writeRBACCMFile writes content to an argocd-rbac-cm file, preserving
// existing file permissions when the file already exists.
// The path is canonicalized to prevent symlink-based path traversal.
func writeRBACCMFile(path, content string) error {
	safePath, err := fsutil.EvalCanonicalPath(path)
	if err != nil {
		return fmt.Errorf("resolving canonical path for %s: %w", path, err)
	}

	perm := os.FileMode(rbacCMFilePermissions)

	info, statErr := os.Stat(safePath)
	if statErr == nil {
		perm = info.Mode().Perm()
	}

	//nolint:gosec // safePath is canonicalized via fsutil.EvalCanonicalPath
	err = os.WriteFile(safePath, []byte(content), perm)
	if err != nil {
		return fmt.Errorf("writing %s: %w", safePath, err)
	}

	return nil
}
