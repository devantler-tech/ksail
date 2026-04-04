package tenant

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"
)

func TestDelete_RemovesTenantDirectory(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	tenantDir := filepath.Join(tmpDir, "my-tenant")
	require.NoError(t, os.MkdirAll(tenantDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(tenantDir, "namespace.yaml"), []byte("test"), 0o644))

	err := Delete(DeleteOptions{
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

	err := Delete(DeleteOptions{
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
	require.NoError(t, os.WriteFile(filepath.Join(tenantDir, "namespace.yaml"), []byte("test"), 0o644))

	kustomContent := `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
- my-tenant
- other-tenant
`
	kPath := filepath.Join(tmpDir, "kustomization.yaml")
	require.NoError(t, os.WriteFile(kPath, []byte(kustomContent), 0o644))

	err := Delete(DeleteOptions{
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
	data, readErr := os.ReadFile(kPath)
	require.NoError(t, readErr)

	var raw map[string]any
	require.NoError(t, yaml.Unmarshal(data, &raw))
	resources := getResources(raw)
	require.NotContains(t, resources, "my-tenant")
	require.Contains(t, resources, "other-tenant")
}

func TestDelete_ContinuesIfNoKustomizationFound(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	tenantDir := filepath.Join(tmpDir, "my-tenant")
	require.NoError(t, os.MkdirAll(tenantDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(tenantDir, "namespace.yaml"), []byte("test"), 0o644))

	err := Delete(DeleteOptions{
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

	err := Delete(DeleteOptions{
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

	err := Delete(DeleteOptions{
		Name:        "my-tenant",
		OutputDir:   tmpDir,
		DeleteRepo:  true,
		GitProvider: "github",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "--git-repo")
}
