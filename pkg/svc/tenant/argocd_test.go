package tenant_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	"github.com/devantler-tech/ksail/v7/pkg/svc/tenant"
	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/stretchr/testify/require"
)

const rbacCMHeader = "apiVersion: v1"

func TestGenerateArgoCDManifests_ProjectSingleNamespace(t *testing.T) {
	t.Parallel()

	opts := tenant.Options{
		Name:        "team-alpha",
		Namespaces:  []string{"team-alpha"},
		TenantType:  tenant.TypeArgoCD,
		GitProvider: "github",
		TenantRepo:  "org/team-alpha-app",
	}
	result, err := tenant.GenerateArgoCDManifests(opts)
	require.NoError(t, err)
	snaps.MatchSnapshot(t, result["project.yaml"])
}

func TestGenerateArgoCDManifests_ProjectMultipleNamespaces(t *testing.T) {
	t.Parallel()

	opts := tenant.Options{
		Name:        "team-beta",
		Namespaces:  []string{"team-beta-dev", "team-beta-staging", "team-beta-prod"},
		TenantType:  tenant.TypeArgoCD,
		GitProvider: "github",
		TenantRepo:  "org/team-beta-app",
	}
	result, err := tenant.GenerateArgoCDManifests(opts)
	require.NoError(t, err)
	snaps.MatchSnapshot(t, result["project.yaml"])
}

func TestGenerateArgoCDManifests_AppGitHub(t *testing.T) {
	t.Parallel()

	opts := tenant.Options{
		Name:        "team-alpha",
		Namespaces:  []string{"team-alpha"},
		TenantType:  tenant.TypeArgoCD,
		GitProvider: "github",
		TenantRepo:  "org/team-alpha-app",
	}
	result, err := tenant.GenerateArgoCDManifests(opts)
	require.NoError(t, err)
	snaps.MatchSnapshot(t, result["app.yaml"])
}

func TestGenerateArgoCDManifests_AppGitLab(t *testing.T) {
	t.Parallel()

	opts := tenant.Options{
		Name:        "team-gamma",
		Namespaces:  []string{"team-gamma"},
		TenantType:  tenant.TypeArgoCD,
		GitProvider: "gitlab",
		TenantRepo:  "org/team-gamma-app",
	}
	result, err := tenant.GenerateArgoCDManifests(opts)
	require.NoError(t, err)
	snaps.MatchSnapshot(t, result["app.yaml"])
}

func TestGenerateArgoCDManifests_NoRBACConfigMap(t *testing.T) {
	t.Parallel()

	opts := tenant.Options{
		Name:        "team-alpha",
		Namespaces:  []string{"team-alpha"},
		TenantType:  tenant.TypeArgoCD,
		GitProvider: "github",
		TenantRepo:  "org/team-alpha-app",
	}
	result, err := tenant.GenerateArgoCDManifests(opts)
	require.NoError(t, err)
	// argocd-rbac-cm.yaml should not be generated per-tenant.
	_, exists := result["argocd-rbac-cm.yaml"]
	require.False(t, exists, "argocd-rbac-cm.yaml should not be generated per-tenant")
}

func TestMergeArgoCDRBACPolicy_EmptyExisting(t *testing.T) {
	t.Parallel()

	result, err := tenant.MergeArgoCDRBACPolicy("", "team-alpha")
	require.NoError(t, err)
	snaps.MatchSnapshot(t, result)
}

func TestMergeArgoCDRBACPolicy_ExistingSameTenant(t *testing.T) {
	t.Parallel()

	existing := rbacCMHeader + `
kind: ConfigMap
metadata:
  name: argocd-rbac-cm
  namespace: argocd
  labels:
    app.kubernetes.io/managed-by: ksail
data:
  policy.csv: |
    p, role:team-alpha, applications, *, team-alpha/*, allow
    p, role:team-alpha, projects, get, team-alpha, allow
    g, team-alpha, role:team-alpha
`
	result, err := tenant.MergeArgoCDRBACPolicy(existing, "team-alpha")
	require.NoError(t, err)
	require.Contains(t, result, "role:team-alpha")
	// Should not duplicate the policy lines
	snaps.MatchSnapshot(t, result)
}

func TestMergeArgoCDRBACPolicy_ExistingOtherTenant(t *testing.T) {
	t.Parallel()

	existing := rbacCMHeader + `
kind: ConfigMap
metadata:
  name: argocd-rbac-cm
  namespace: argocd
  labels:
    app.kubernetes.io/managed-by: ksail
data:
  policy.csv: |
    p, role:team-alpha, applications, *, team-alpha/*, allow
    p, role:team-alpha, projects, get, team-alpha, allow
    g, team-alpha, role:team-alpha
`
	result, err := tenant.MergeArgoCDRBACPolicy(existing, "team-beta")
	require.NoError(t, err)
	require.Contains(t, result, "role:team-alpha")
	require.Contains(t, result, "role:team-beta")
	snaps.MatchSnapshot(t, result)
}

func TestRemoveArgoCDRBACPolicy_RemovesTargetTenant(t *testing.T) {
	t.Parallel()

	existing := rbacCMHeader + `
kind: ConfigMap
metadata:
  name: argocd-rbac-cm
  namespace: argocd
  labels:
    app.kubernetes.io/managed-by: ksail
data:
  policy.csv: |
    p, role:team-alpha, applications, *, team-alpha/*, allow
    p, role:team-alpha, projects, get, team-alpha, allow
    g, team-alpha, role:team-alpha
`
	result, err := tenant.RemoveArgoCDRBACPolicy(existing, "team-alpha")
	require.NoError(t, err)
	require.NotContains(t, result, "role:team-alpha")
	snaps.MatchSnapshot(t, result)
}

func TestMergeArgoCDRBACPolicy_SubstringTenantNames(t *testing.T) {
	t.Parallel()

	existing := rbacCMHeader + `
kind: ConfigMap
metadata:
  name: argocd-rbac-cm
  namespace: argocd
  labels:
    app.kubernetes.io/managed-by: ksail
data:
  policy.csv: |
    p, role:team, applications, *, team/*, allow
    p, role:team, projects, get, team, allow
    g, team, role:team
`
	// "team-alpha" must be added even though "team" is a substring of "team-alpha".
	result, err := tenant.MergeArgoCDRBACPolicy(existing, "team-alpha")
	require.NoError(t, err)
	require.Contains(t, result, "role:team,")
	require.Contains(t, result, "role:team-alpha,")
}

func TestRemoveArgoCDRBACPolicy_SubstringTenantNames(t *testing.T) {
	t.Parallel()

	existing := rbacCMHeader + `
kind: ConfigMap
metadata:
  name: argocd-rbac-cm
  namespace: argocd
data:
  policy.csv: |
    p, role:team, applications, *, team/*, allow
    p, role:team, projects, get, team, allow
    g, team, role:team
    p, role:team-alpha, applications, *, team-alpha/*, allow
    p, role:team-alpha, projects, get, team-alpha, allow
    g, team-alpha, role:team-alpha
`
	// Removing "team" should NOT remove "team-alpha" lines.
	result, err := tenant.RemoveArgoCDRBACPolicy(existing, "team")
	require.NoError(t, err)
	require.NotContains(t, result, "role:team,")
	require.Contains(t, result, "role:team-alpha,")
}

func TestRemoveArgoCDRBACPolicy_PreservesOtherTenant(t *testing.T) {
	t.Parallel()

	existing := rbacCMHeader + `
kind: ConfigMap
metadata:
  name: argocd-rbac-cm
  namespace: argocd
  labels:
    app.kubernetes.io/managed-by: ksail
data:
  policy.csv: |
    p, role:team-alpha, applications, *, team-alpha/*, allow
    p, role:team-alpha, projects, get, team-alpha, allow
    g, team-alpha, role:team-alpha
    p, role:team-beta, applications, *, team-beta/*, allow
    p, role:team-beta, projects, get, team-beta, allow
    g, team-beta, role:team-beta
`
	result, err := tenant.RemoveArgoCDRBACPolicy(existing, "team-alpha")
	require.NoError(t, err)
	require.NotContains(t, result, "role:team-alpha")
	require.Contains(t, result, "role:team-beta")
	snaps.MatchSnapshot(t, result)
}

// --- FindArgoCDRBACCM tests ---

func TestFindArgoCDRBACCM_Found(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// Write a ConfigMap named argocd-rbac-cm with a non-obvious filename.
	content := `apiVersion: v1
kind: ConfigMap
metadata:
  name: argocd-rbac-cm
  namespace: argocd
data:
  policy.csv: ""
`
	err := os.WriteFile(filepath.Join(dir, "my-custom-rbac.yaml"), []byte(content), 0o600)
	require.NoError(t, err)

	found, err := tenant.FindArgoCDRBACCM(dir)
	require.NoError(t, err)

	canonDir, err := fsutil.EvalCanonicalPath(dir)
	require.NoError(t, err)
	require.Equal(t, filepath.Join(canonDir, "my-custom-rbac.yaml"), found)
}

func TestFindArgoCDRBACCM_NotFound(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// Write a ConfigMap with a different name.
	content := `apiVersion: v1
kind: ConfigMap
metadata:
  name: some-other-cm
data: {}
`
	err := os.WriteFile(filepath.Join(dir, "configmap.yaml"), []byte(content), 0o600)
	require.NoError(t, err)

	found, err := tenant.FindArgoCDRBACCM(dir)
	require.NoError(t, err)
	require.Empty(t, found)
}

func TestFindArgoCDRBACCM_IgnoresNonYAML(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// Write a JSON file that would match if parsed, but has no YAML extension.
	content := `{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"argocd-rbac-cm"}}`
	err := os.WriteFile(filepath.Join(dir, "rbac.json"), []byte(content), 0o600)
	require.NoError(t, err)

	found, err := tenant.FindArgoCDRBACCM(dir)
	require.NoError(t, err)
	require.Empty(t, found)
}

func TestFindArgoCDRBACCM_MultiDocYAML(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// Multi-document YAML with argocd-rbac-cm as the second document.
	content := `apiVersion: v1
kind: Namespace
metadata:
  name: argocd
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: argocd-rbac-cm
  namespace: argocd
data:
  policy.csv: ""
`
	err := os.WriteFile(filepath.Join(dir, "argocd-resources.yaml"), []byte(content), 0o600)
	require.NoError(t, err)

	found, err := tenant.FindArgoCDRBACCM(dir)
	require.NoError(t, err)

	canonDir, err := fsutil.EvalCanonicalPath(dir)
	require.NoError(t, err)
	require.Equal(t, filepath.Join(canonDir, "argocd-resources.yaml"), found)
}

// --- MergeArgoCDRBACPolicyFile tests ---

func TestMergeArgoCDRBACPolicyFile_NewFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	rbacCMPath := filepath.Join(dir, "argocd-rbac-cm.yaml")

	err := tenant.MergeArgoCDRBACPolicyFile(rbacCMPath, "team-alpha")
	require.NoError(t, err)

	data, err := os.ReadFile(rbacCMPath) //nolint:gosec // test-only path from t.TempDir()
	require.NoError(t, err)
	require.Contains(t, string(data), "role:team-alpha")
	require.Contains(t, string(data), "argocd-rbac-cm")
}

func TestMergeArgoCDRBACPolicyFile_ExistingFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	rbacCMPath := filepath.Join(dir, "argocd-rbac-cm.yaml")

	existing := `apiVersion: v1
kind: ConfigMap
metadata:
  name: argocd-rbac-cm
  namespace: argocd
data:
  policy.csv: |
    p, role:team-alpha, applications, *, team-alpha/*, allow
    p, role:team-alpha, projects, get, team-alpha, allow
    g, team-alpha, role:team-alpha
`
	err := os.WriteFile(rbacCMPath, []byte(existing), 0o600)
	require.NoError(t, err)

	err = tenant.MergeArgoCDRBACPolicyFile(rbacCMPath, "team-beta")
	require.NoError(t, err)

	data, err := os.ReadFile(rbacCMPath) //nolint:gosec // test-only path from t.TempDir()
	require.NoError(t, err)
	require.Contains(t, string(data), "role:team-alpha")
	require.Contains(t, string(data), "role:team-beta")
}

func TestMergeArgoCDRBACPolicyFile_Idempotent(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	rbacCMPath := filepath.Join(dir, "argocd-rbac-cm.yaml")

	existing := `apiVersion: v1
kind: ConfigMap
metadata:
  name: argocd-rbac-cm
  namespace: argocd
data:
  policy.csv: |
    p, role:team-alpha, applications, *, team-alpha/*, allow
    p, role:team-alpha, projects, get, team-alpha, allow
    g, team-alpha, role:team-alpha
`
	err := os.WriteFile(rbacCMPath, []byte(existing), 0o600)
	require.NoError(t, err)

	err = tenant.MergeArgoCDRBACPolicyFile(rbacCMPath, "team-alpha")
	require.NoError(t, err)

	data, err := os.ReadFile(rbacCMPath) //nolint:gosec // test-only path from t.TempDir()
	require.NoError(t, err)

	// Should not duplicate the policy lines.
	content := string(data)
	require.Contains(t, content, "role:team-alpha")

	// Count occurrences of "role:team-alpha," — should appear exactly twice (two p lines).
	count := 0

	needle := "role:team-alpha,"
	for i := 0; i <= len(content)-len(needle); i++ {
		if content[i:i+len(needle)] == needle {
			count++
		}
	}

	require.Equal(t, 2, count, "policy lines should not be duplicated")
}

// --- RemoveArgoCDRBACPolicyFile tests ---

func TestRemoveArgoCDRBACPolicyFile_ExistingFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	rbacCMPath := filepath.Join(dir, "argocd-rbac-cm.yaml")

	existing := `apiVersion: v1
kind: ConfigMap
metadata:
  name: argocd-rbac-cm
  namespace: argocd
data:
  policy.csv: |
    p, role:team-alpha, applications, *, team-alpha/*, allow
    p, role:team-alpha, projects, get, team-alpha, allow
    g, team-alpha, role:team-alpha
    p, role:team-beta, applications, *, team-beta/*, allow
    p, role:team-beta, projects, get, team-beta, allow
    g, team-beta, role:team-beta
`
	err := os.WriteFile(rbacCMPath, []byte(existing), 0o600)
	require.NoError(t, err)

	err = tenant.RemoveArgoCDRBACPolicyFile(rbacCMPath, "team-alpha")
	require.NoError(t, err)

	data, err := os.ReadFile(rbacCMPath) //nolint:gosec // test-only path from t.TempDir()
	require.NoError(t, err)
	require.NotContains(t, string(data), "role:team-alpha")
	require.Contains(t, string(data), "role:team-beta")
}

func TestRemoveArgoCDRBACPolicyFile_FileNotExist(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	rbacCMPath := filepath.Join(dir, "nonexistent.yaml")

	err := tenant.RemoveArgoCDRBACPolicyFile(rbacCMPath, "team-alpha")
	require.NoError(t, err, "should be a no-op when file does not exist")
}
