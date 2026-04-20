package tenant_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/tenant"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"
)

func TestDelete_RemovesTenantDirectory(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	tenantDir := filepath.Join(tmpDir, "my-tenant")
	require.NoError(t, os.MkdirAll(tenantDir, 0o750))
	require.NoError(t, os.WriteFile(
		filepath.Join(tenantDir, "namespace.yaml"),
		[]byte("test"), 0o600,
	))

	err := tenant.Delete(context.Background(), tenant.DeleteOptions{
		Name:      "my-tenant",
		OutputDir: tmpDir,
	})
	require.NoError(t, err)

	_, statErr := os.Stat(tenantDir)
	require.True(t, os.IsNotExist(statErr), "tenant directory should be removed")
}

func TestDelete_ErrorForNonExistentDirectory(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	err := tenant.Delete(context.Background(), tenant.DeleteOptions{
		Name:      "no-such-tenant",
		OutputDir: tmpDir,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "does not exist")
}

func TestDelete_WithUnregister(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	tenantDir := filepath.Join(tmpDir, "my-tenant")
	require.NoError(t, os.MkdirAll(tenantDir, 0o750))
	require.NoError(
		t,
		os.WriteFile(filepath.Join(tenantDir, "namespace.yaml"), []byte("test"), 0o600),
	)

	kustomContent := `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
- my-tenant
- other-tenant
`
	kPath := filepath.Join(tmpDir, "kustomization.yaml")
	require.NoError(t, os.WriteFile(kPath, []byte(kustomContent), 0o600))

	err := tenant.Delete(context.Background(), tenant.DeleteOptions{
		Name:              "my-tenant",
		OutputDir:         tmpDir,
		Unregister:        true,
		KustomizationPath: kPath,
	})
	require.NoError(t, err)

	// Tenant directory should be removed.
	_, statErr := os.Stat(tenantDir)
	require.True(t, os.IsNotExist(statErr), "tenant directory should be removed")

	// Kustomization should no longer reference my-tenant.
	data, readErr := os.ReadFile(kPath) //nolint:gosec // test path
	require.NoError(t, readErr)

	var raw map[string]any
	require.NoError(t, yaml.Unmarshal(data, &raw))
	resources := tenant.ExportGetResources(raw)
	require.NotContains(t, resources, "my-tenant")
	require.Contains(t, resources, "other-tenant")
}

func TestDelete_ContinuesIfNoKustomizationFound(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	tenantDir := filepath.Join(tmpDir, "my-tenant")
	require.NoError(t, os.MkdirAll(tenantDir, 0o750))
	require.NoError(
		t,
		os.WriteFile(filepath.Join(tenantDir, "namespace.yaml"), []byte("test"), 0o600),
	)

	err := tenant.Delete(context.Background(), tenant.DeleteOptions{
		Name:       "my-tenant",
		OutputDir:  tmpDir,
		Unregister: true,
	})
	require.NoError(t, err)

	_, statErr := os.Stat(tenantDir)
	require.True(t, os.IsNotExist(statErr), "tenant directory should still be removed")
}

func TestDelete_DeleteRepoRequiresGitProvider(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	tenantDir := filepath.Join(tmpDir, "my-tenant")
	require.NoError(t, os.MkdirAll(tenantDir, 0o750))

	err := tenant.Delete(context.Background(), tenant.DeleteOptions{
		Name:       "my-tenant",
		OutputDir:  tmpDir,
		DeleteRepo: true,
		TenantRepo: "org/my-tenant",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "--git-provider")
}

func TestDelete_DeleteRepoRequiresGitRepo(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	tenantDir := filepath.Join(tmpDir, "my-tenant")
	require.NoError(t, os.MkdirAll(tenantDir, 0o750))

	err := tenant.Delete(context.Background(), tenant.DeleteOptions{
		Name:        "my-tenant",
		OutputDir:   tmpDir,
		DeleteRepo:  true,
		GitProvider: "github",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "--tenant-repo")
}

func TestDelete_InvalidGitRepoFormat(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	tenantDir := filepath.Join(tmpDir, "my-tenant")
	require.NoError(t, os.MkdirAll(tenantDir, 0o750))

	err := tenant.Delete(context.Background(), tenant.DeleteOptions{
		Name:        "my-tenant",
		OutputDir:   tmpDir,
		DeleteRepo:  true,
		GitProvider: "github",
		TenantRepo:  "invalid-no-slash",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "parse tenant-repo")
}

func TestDelete_UnsupportedGitProvider(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	tenantDir := filepath.Join(tmpDir, "my-tenant")
	require.NoError(t, os.MkdirAll(tenantDir, 0o750))

	err := tenant.Delete(context.Background(), tenant.DeleteOptions{
		Name:        "my-tenant",
		OutputDir:   tmpDir,
		DeleteRepo:  true,
		GitProvider: "bitbucket",
		TenantRepo:  "org/my-tenant",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "create git provider")
}

func TestDelete_PathTraversalName(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	err := tenant.Delete(context.Background(), tenant.DeleteOptions{
		Name:      "../escape",
		OutputDir: tmpDir,
	})
	require.Error(t, err)
	require.ErrorIs(t, err, tenant.ErrInvalidTenantName)
}

func TestDelete_UnregisterWithExplicitKustomizationPath(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	tenantDir := filepath.Join(tmpDir, "my-tenant")
	require.NoError(t, os.MkdirAll(tenantDir, 0o750))
	require.NoError(
		t,
		os.WriteFile(filepath.Join(tenantDir, "namespace.yaml"), []byte("test"), 0o600),
	)

	kustomContent := `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
- my-tenant
`
	kPath := filepath.Join(tmpDir, "kustomization.yaml")
	require.NoError(t, os.WriteFile(kPath, []byte(kustomContent), 0o600))

	err := tenant.Delete(context.Background(), tenant.DeleteOptions{
		Name:              "my-tenant",
		OutputDir:         tmpDir,
		Unregister:        true,
		KustomizationPath: kPath,
	})
	require.NoError(t, err)

	_, statErr := os.Stat(tenantDir)
	require.True(t, os.IsNotExist(statErr))

	data, readErr := os.ReadFile(kPath) //nolint:gosec // test path
	require.NoError(t, readErr)

	var raw map[string]any
	require.NoError(t, yaml.Unmarshal(data, &raw))
	resources := tenant.ExportGetResources(raw)
	require.NotContains(t, resources, "my-tenant")
}

// --- ArgoCD RBAC cleanup tests ---

const rbacCMContent = `apiVersion: v1
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

const rbacCMTwoTenants = `apiVersion: v1
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

func createArgoCDTenantDir(t *testing.T, base, name string) string {
	t.Helper()

	dir := filepath.Join(base, name)
	require.NoError(t, os.MkdirAll(dir, 0o750))
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "project.yaml"),
		[]byte("apiVersion: argoproj.io/v1alpha1\nkind: AppProject\n"),
		0o600,
	))
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "namespace.yaml"),
		[]byte("test"),
		0o600,
	))

	return dir
}

func TestDelete_ArgoCDTenantRemovesRBACPolicy(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	createArgoCDTenantDir(t, tmpDir, "team-alpha")

	rbacPath := filepath.Join(tmpDir, "rbac.yaml")
	require.NoError(t, os.WriteFile(rbacPath, []byte(rbacCMContent), 0o600))

	err := tenant.Delete(context.Background(), tenant.DeleteOptions{
		Name:       "team-alpha",
		OutputDir:  tmpDir,
		Unregister: false,
	})
	require.NoError(t, err)

	data, readErr := os.ReadFile(rbacPath) //nolint:gosec // test path
	require.NoError(t, readErr)
	require.NotContains(t, string(data), "role:team-alpha")
}

func TestDelete_ArgoCDTenantPreservesOtherTenantPolicy(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	createArgoCDTenantDir(t, tmpDir, "team-alpha")

	rbacPath := filepath.Join(tmpDir, "shared-rbac.yml")
	require.NoError(t, os.WriteFile(rbacPath, []byte(rbacCMTwoTenants), 0o600))

	err := tenant.Delete(context.Background(), tenant.DeleteOptions{
		Name:       "team-alpha",
		OutputDir:  tmpDir,
		Unregister: false,
	})
	require.NoError(t, err)

	data, readErr := os.ReadFile(rbacPath) //nolint:gosec // test path
	require.NoError(t, readErr)
	require.NotContains(t, string(data), "role:team-alpha")
	require.Contains(t, string(data), "role:team-beta")
}

func TestDelete_ArgoCDTenantWithoutRBACFileContinues(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	tenantDir := createArgoCDTenantDir(t, tmpDir, "team-gamma")

	err := tenant.Delete(context.Background(), tenant.DeleteOptions{
		Name:       "team-gamma",
		OutputDir:  tmpDir,
		Unregister: false,
	})
	require.NoError(t, err)

	_, statErr := os.Stat(tenantDir)
	require.True(t, os.IsNotExist(statErr), "tenant directory should be removed")
}

func TestDelete_NonArgoCDTenantDoesNotModifyRBACFile(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Non-ArgoCD tenant (no project.yaml).
	tenantDir := filepath.Join(tmpDir, "my-tenant")
	require.NoError(t, os.MkdirAll(tenantDir, 0o750))
	require.NoError(t, os.WriteFile(
		filepath.Join(tenantDir, "namespace.yaml"),
		[]byte("test"), 0o600,
	))

	rbacPath := filepath.Join(tmpDir, "rbac-config.yaml")
	require.NoError(t, os.WriteFile(rbacPath, []byte(rbacCMContent), 0o600))

	err := tenant.Delete(context.Background(), tenant.DeleteOptions{
		Name:       "my-tenant",
		OutputDir:  tmpDir,
		Unregister: false,
	})
	require.NoError(t, err)

	data, readErr := os.ReadFile(rbacPath) //nolint:gosec // test path
	require.NoError(t, readErr)
	require.Contains(t, string(data), "role:team-alpha",
		"RBAC file should not be modified for non-ArgoCD tenants")
}

func TestDelete_ArgoCDTenantFindsRBACByContent(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	createArgoCDTenantDir(t, tmpDir, "team-alpha")

	// Use an unusual filename to prove content-based discovery.
	rbacPath := filepath.Join(tmpDir, "my-custom-rbac-policies.yaml")
	require.NoError(t, os.WriteFile(rbacPath, []byte(rbacCMContent), 0o600))

	// Also place a non-matching YAML file to ensure it's skipped.
	otherPath := filepath.Join(tmpDir, "aaa-first-file.yaml")
	require.NoError(t, os.WriteFile(otherPath, []byte("apiVersion: v1\nkind: Namespace\n"), 0o600))

	err := tenant.Delete(context.Background(), tenant.DeleteOptions{
		Name:       "team-alpha",
		OutputDir:  tmpDir,
		Unregister: false,
	})
	require.NoError(t, err)

	data, readErr := os.ReadFile(rbacPath) //nolint:gosec // test path
	require.NoError(t, readErr)
	require.NotContains(t, string(data), "role:team-alpha")
}

func TestDelete_ArgoCDTenantFindsRBACInMultiDocYAML(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	createArgoCDTenantDir(t, tmpDir, "team-alpha")

	// Multi-document YAML: argocd-rbac-cm is the second document.
	multiDoc := "apiVersion: v1\nkind: Namespace\nmetadata:\n  name: argocd\n---\n" + rbacCMContent
	rbacPath := filepath.Join(tmpDir, "argocd-resources.yaml")
	require.NoError(t, os.WriteFile(rbacPath, []byte(multiDoc), 0o600))

	err := tenant.Delete(context.Background(), tenant.DeleteOptions{
		Name:       "team-alpha",
		OutputDir:  tmpDir,
		Unregister: false,
	})
	require.NoError(t, err)

	data, readErr := os.ReadFile(rbacPath) //nolint:gosec // test path
	require.NoError(t, readErr)
	require.NotContains(t, string(data), "role:team-alpha")
}

func TestDelete_ArgoCDTenantFindsRBACInLeadingSeparatorYAML(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	createArgoCDTenantDir(t, tmpDir, "team-alpha")

	// YAML file starts with --- (common Kubernetes manifest pattern).
	leadingDoc := "---\n" + rbacCMContent
	rbacPath := filepath.Join(tmpDir, "rbac.yaml")
	require.NoError(t, os.WriteFile(rbacPath, []byte(leadingDoc), 0o600))

	err := tenant.Delete(context.Background(), tenant.DeleteOptions{
		Name:       "team-alpha",
		OutputDir:  tmpDir,
		Unregister: false,
	})
	require.NoError(t, err)

	data, readErr := os.ReadFile(rbacPath) //nolint:gosec // test path
	require.NoError(t, readErr)
	require.NotContains(t, string(data), "role:team-alpha")
}
