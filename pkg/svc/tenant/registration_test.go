package tenant

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"
)

func writeKustomizationFile(t *testing.T, dir, content string) string {
	t.Helper()
	path := filepath.Join(dir, "kustomization.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return path
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

	var k kustomizationType
	data, _ := os.ReadFile(kPath)
	require.NoError(t, unmarshalYAML(data, &k))
	require.Contains(t, k.Resources, "existing-tenant")
	require.Contains(t, k.Resources, "new-tenant")
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

	var k kustomizationType
	data, _ := os.ReadFile(kPath)
	require.NoError(t, unmarshalYAML(data, &k))
	require.Equal(t, []string{"team-alpha"}, k.Resources)
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

	var k kustomizationType
	data, _ := os.ReadFile(kPath)
	require.NoError(t, unmarshalYAML(data, &k))

	count := 0
	for _, r := range k.Resources {
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

	var k kustomizationType
	data, _ := os.ReadFile(kPath)
	require.NoError(t, unmarshalYAML(data, &k))
	require.NotContains(t, k.Resources, "team-alpha")
	require.Contains(t, k.Resources, "team-beta")
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

	var k kustomizationType
	data, _ := os.ReadFile(kPath)
	require.NoError(t, unmarshalYAML(data, &k))
	require.Equal(t, []string{"team-beta"}, k.Resources)
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
	require.Equal(t, filepath.Join(root, "kustomization.yaml"), found)
}

func TestFindKustomizationReturnsErrWhenNotFound(t *testing.T) {
	t.Parallel()
	deep := t.TempDir()
	nested := filepath.Join(deep, "x", "y", "z")
	require.NoError(t, os.MkdirAll(nested, 0o755))

	_, err := FindKustomization(nested)
	require.ErrorIs(t, err, ErrKustomizationNotFound)
}

// unmarshalYAML is a test helper wrapping sigs.k8s.io/yaml.
func unmarshalYAML(data []byte, v interface{}) error {
	return yaml.Unmarshal(data, v)
}
