package tenant_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/svc/tenant"
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

	err := tenant.Delete(tenant.DeleteOptions{
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

	err := tenant.Delete(tenant.DeleteOptions{
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

	err := tenant.Delete(tenant.DeleteOptions{
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

	err := tenant.Delete(tenant.DeleteOptions{
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

	err := tenant.Delete(tenant.DeleteOptions{
		Name:       "my-tenant",
		OutputDir:  tmpDir,
		DeleteRepo: true,
		GitRepo:    "org/my-tenant",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "--git-provider")
}

func TestDelete_DeleteRepoRequiresGitRepo(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	tenantDir := filepath.Join(tmpDir, "my-tenant")
	require.NoError(t, os.MkdirAll(tenantDir, 0o750))

	err := tenant.Delete(tenant.DeleteOptions{
		Name:        "my-tenant",
		OutputDir:   tmpDir,
		DeleteRepo:  true,
		GitProvider: "github",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "--git-repo")
}

func TestDelete_InvalidGitRepoFormat(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	tenantDir := filepath.Join(tmpDir, "my-tenant")
	require.NoError(t, os.MkdirAll(tenantDir, 0o750))

	err := tenant.Delete(tenant.DeleteOptions{
		Name:        "my-tenant",
		OutputDir:   tmpDir,
		DeleteRepo:  true,
		GitProvider: "github",
		GitRepo:     "invalid-no-slash",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "parse git-repo")
}

func TestDelete_UnsupportedGitProvider(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	tenantDir := filepath.Join(tmpDir, "my-tenant")
	require.NoError(t, os.MkdirAll(tenantDir, 0o750))

	err := tenant.Delete(tenant.DeleteOptions{
		Name:        "my-tenant",
		OutputDir:   tmpDir,
		DeleteRepo:  true,
		GitProvider: "bitbucket",
		GitRepo:     "org/my-tenant",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "create git provider")
}

func TestDelete_PathTraversalName(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	err := tenant.Delete(tenant.DeleteOptions{
		Name:      "../escape",
		OutputDir: tmpDir,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "must not contain path separators")
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

	err := tenant.Delete(tenant.DeleteOptions{
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
