package tenant

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/fsutil"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"
)

func writeKustomizationFile(t *testing.T, dir, content string) string {
	t.Helper()
	path := filepath.Join(dir, "kustomization.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return path
}

func readKustomizationResources(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var raw map[string]any
	require.NoError(t, yaml.Unmarshal(data, &raw))
	return getResources(raw)
}

func TestRegisterTenantAddsToExistingResources(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	kPath := writeKustomizationFile(t, dir, `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
- existing-tenant
`)

	err := RegisterTenant("new-tenant", dir, kPath)
	require.NoError(t, err)

	resources := readKustomizationResources(t, kPath)
	require.Contains(t, resources, "existing-tenant")
	require.Contains(t, resources, "new-tenant")
}

func TestRegisterTenantCreatesResourcesEntry(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	kPath := writeKustomizationFile(t, dir, `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources: []
`)

	err := RegisterTenant("team-alpha", dir, kPath)
	require.NoError(t, err)

	resources := readKustomizationResources(t, kPath)
	require.Equal(t, []string{"team-alpha"}, resources)
}

func TestRegisterTenantIsIdempotent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	kPath := writeKustomizationFile(t, dir, `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
- team-alpha
`)

	err := RegisterTenant("team-alpha", dir, kPath)
	require.NoError(t, err)

	resources := readKustomizationResources(t, kPath)
	count := 0
	for _, r := range resources {
		if r == "team-alpha" {
			count++
		}
	}
	require.Equal(t, 1, count, "should not duplicate the entry")
}

func TestUnregisterTenantRemovesTenant(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	kPath := writeKustomizationFile(t, dir, `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
- team-alpha
- team-beta
`)

	err := UnregisterTenant("team-alpha", dir, kPath)
	require.NoError(t, err)

	resources := readKustomizationResources(t, kPath)
	require.NotContains(t, resources, "team-alpha")
	require.Contains(t, resources, "team-beta")
}

func TestUnregisterTenantSafeWhenNotPresent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	kPath := writeKustomizationFile(t, dir, `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
- team-beta
`)

	err := UnregisterTenant("nonexistent", dir, kPath)
	require.NoError(t, err)

	resources := readKustomizationResources(t, kPath)
	require.Equal(t, []string{"team-beta"}, resources)
}

func TestRegisterTenantPreservesOtherFields(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	kPath := writeKustomizationFile(t, dir, `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
namespace: production
namePrefix: prod-
resources:
- existing
`)

	err := RegisterTenant("new-tenant", dir, kPath)
	require.NoError(t, err)

	data, err := os.ReadFile(kPath)
	require.NoError(t, err)
	var raw map[string]any
	require.NoError(t, yaml.Unmarshal(data, &raw))

	// Other fields must be preserved.
	require.Equal(t, "production", raw["namespace"])
	require.Equal(t, "prod-", raw["namePrefix"])
	resources := getResources(raw)
	require.Contains(t, resources, "existing")
	require.Contains(t, resources, "new-tenant")
}

func TestFindKustomizationWalksUp(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeKustomizationFile(t, root, `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources: []
`)

	nested := filepath.Join(root, "a", "b", "c")
	require.NoError(t, os.MkdirAll(nested, 0o755))

	found, err := FindKustomization(nested)
	require.NoError(t, err)

	// EvalCanonicalPath resolves symlinks (e.g., /var → /private/var on macOS).
	canonicalRoot, err := fsutil.EvalCanonicalPath(root)
	require.NoError(t, err)
	require.Equal(t, filepath.Join(canonicalRoot, "kustomization.yaml"), found)
}

func TestFindKustomizationReturnsErrWhenNotFound(t *testing.T) {
	t.Parallel()
	deep := t.TempDir()
	nested := filepath.Join(deep, "x", "y", "z")
	require.NoError(t, os.MkdirAll(nested, 0o755))

	_, err := FindKustomization(nested)
	require.ErrorIs(t, err, ErrKustomizationNotFound)
}
