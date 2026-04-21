package tenant_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	"github.com/devantler-tech/ksail/v7/pkg/svc/tenant"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"
)

func writeKustomizationFile(t *testing.T, dir, content string) string {
	t.Helper()

	path := filepath.Join(dir, "kustomization.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	return path
}

func readKustomizationResources(t *testing.T, path string) []string {
	t.Helper()

	data, err := os.ReadFile(path) //nolint:gosec // test path
	require.NoError(t, err)

	var raw map[string]any
	require.NoError(t, yaml.Unmarshal(data, &raw))

	return tenant.ExportGetResources(raw)
}

func TestRegisterTenantAddsToExistingResources(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	kPath := writeKustomizationFile(t, dir, `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
- existing-tenant
`)

	err := tenant.RegisterTenant("new-tenant", dir, kPath)
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

	err := tenant.RegisterTenant("team-alpha", dir, kPath)
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

	err := tenant.RegisterTenant("team-alpha", dir, kPath)
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

	err := tenant.UnregisterTenant("team-alpha", dir, kPath)
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

	err := tenant.UnregisterTenant("nonexistent", dir, kPath)
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

	err := tenant.RegisterTenant("new-tenant", dir, kPath)
	require.NoError(t, err)

	data, err := os.ReadFile(kPath) //nolint:gosec // test path
	require.NoError(t, err)

	var raw map[string]any
	require.NoError(t, yaml.Unmarshal(data, &raw))

	// Other fields must be preserved.
	require.Equal(t, "production", raw["namespace"])
	require.Equal(t, "prod-", raw["namePrefix"])
	resources := tenant.ExportGetResources(raw)
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
	require.NoError(t, os.MkdirAll(nested, 0o750))

	found, err := tenant.FindKustomization(nested)
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
	require.NoError(t, os.MkdirAll(nested, 0o750))

	_, err := tenant.FindKustomization(nested)
	require.ErrorIs(t, err, tenant.ErrKustomizationNotFound)
}

func TestRegisterTenantAutoDiscoversKustomization(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeKustomizationFile(t, root, `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources: []
`)

	// Tenant output dir is a subdirectory.
	subDir := filepath.Join(root, "tenants")
	require.NoError(t, os.MkdirAll(subDir, 0o750))

	err := tenant.RegisterTenant("team-discover", subDir, "")
	require.NoError(t, err)

	kPath := filepath.Join(root, "kustomization.yaml")
	resources := readKustomizationResources(t, kPath)
	require.Contains(t, resources, "tenants/team-discover")
}

func TestRegisterTenantExplicitPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	kPath := writeKustomizationFile(t, dir, `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources: []
`)

	err := tenant.RegisterTenant("explicit-tenant", dir, kPath)
	require.NoError(t, err)

	resources := readKustomizationResources(t, kPath)
	require.Contains(t, resources, "explicit-tenant")
}

func TestRegisterTenantRejectsPathTraversal(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	kPath := writeKustomizationFile(t, root, `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources: []
`)

	// outputDir is outside the kustomization root.
	outsideDir := t.TempDir()

	err := tenant.RegisterTenant("escape-tenant", outsideDir, kPath)
	require.Error(t, err)
	require.Contains(t, err.Error(), "outside the kustomization root")
}

func TestUnregisterTenantAutoDiscoversKustomization(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeKustomizationFile(t, root, `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
- tenants/my-team
`)

	subDir := filepath.Join(root, "tenants")
	require.NoError(t, os.MkdirAll(subDir, 0o750))

	err := tenant.UnregisterTenant("my-team", subDir, "")
	require.NoError(t, err)

	kPath := filepath.Join(root, "kustomization.yaml")
	resources := readKustomizationResources(t, kPath)
	require.NotContains(t, resources, "tenants/my-team")
}

func TestResolveKustomizationPath_ExplicitNotFound(t *testing.T) {
	t.Parallel()

	_, err := tenant.ExportResolveKustomizationPath(".", "/nonexistent/kustomization.yaml")
	require.Error(t, err)
}

// --- RegisterResource tests ---

func TestRegisterResource_AddsToExisting(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	kPath := writeKustomizationFile(t, dir, `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
- tenants/team-alpha
`)

	err := tenant.RegisterResource(kPath, "argocd-rbac-cm.yaml")
	require.NoError(t, err)

	resources := readKustomizationResources(t, kPath)
	require.Contains(t, resources, "argocd-rbac-cm.yaml")
	require.Contains(t, resources, "tenants/team-alpha")
}

func TestRegisterResource_Idempotent(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	kPath := writeKustomizationFile(t, dir, `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
- argocd-rbac-cm.yaml
- tenants/team-alpha
`)

	err := tenant.RegisterResource(kPath, "argocd-rbac-cm.yaml")
	require.NoError(t, err)

	resources := readKustomizationResources(t, kPath)
	count := 0

	for _, r := range resources {
		if r == "argocd-rbac-cm.yaml" {
			count++
		}
	}

	require.Equal(t, 1, count, "resource should not be duplicated")
}

func TestRegisterResource_CreatesResourcesEntry(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	kPath := writeKustomizationFile(t, dir, `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
`)

	err := tenant.RegisterResource(kPath, "argocd-rbac-cm.yaml")
	require.NoError(t, err)

	resources := readKustomizationResources(t, kPath)
	require.Contains(t, resources, "argocd-rbac-cm.yaml")
}
