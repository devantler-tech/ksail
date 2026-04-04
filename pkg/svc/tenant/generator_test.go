package tenant

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/stretchr/testify/require"
)

func TestGenerate_KubectlType(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	err := Generate(Options{
		Name:       "team-kubectl",
		TenantType: TenantTypeKubectl,
		OutputDir:  dir,
	})
	require.NoError(t, err)

	tenantDir := filepath.Join(dir, "team-kubectl")

	// RBAC files must exist.
	for _, f := range []string{"namespace.yaml", "serviceaccount.yaml", "rolebinding.yaml", "kustomization.yaml"} {
		_, err := os.Stat(filepath.Join(tenantDir, f))
		require.NoError(t, err, "expected %s to exist", f)
	}

	// Type-specific files must NOT exist.
	for _, f := range []string{"sync.yaml", "project.yaml", "app.yaml", "argocd-rbac-cm.yaml"} {
		_, err := os.Stat(filepath.Join(tenantDir, f))
		require.ErrorIs(t, err, os.ErrNotExist, "expected %s to not exist", f)
	}

	// Snapshot the kustomization.yaml.
	kustomizationContent, err := os.ReadFile(filepath.Join(tenantDir, "kustomization.yaml"))
	require.NoError(t, err)
	snaps.MatchSnapshot(t, string(kustomizationContent))
}

func TestGenerate_FluxType(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	err := Generate(Options{
		Name:       "team-flux",
		TenantType: TenantTypeFlux,
		OutputDir:  dir,
		SyncSource: SyncSourceOCI,
		Registry:   "oci://ghcr.io",
		GitRepo:    "acme/team-flux-manifests",
	})
	require.NoError(t, err)

	tenantDir := filepath.Join(dir, "team-flux")

	// RBAC + Flux files must exist.
	for _, f := range []string{"namespace.yaml", "serviceaccount.yaml", "rolebinding.yaml", "sync.yaml", "kustomization.yaml"} {
		_, err := os.Stat(filepath.Join(tenantDir, f))
		require.NoError(t, err, "expected %s to exist", f)
	}

	// Verify sync.yaml is in kustomization resources.
	kustomizationContent, err := os.ReadFile(filepath.Join(tenantDir, "kustomization.yaml"))
	require.NoError(t, err)
	require.Contains(t, string(kustomizationContent), "sync.yaml")

	snaps.MatchSnapshot(t, string(kustomizationContent))
}

func TestGenerate_ArgoCDType(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	err := Generate(Options{
		Name:        "team-argocd",
		TenantType:  TenantTypeArgoCD,
		OutputDir:   dir,
		GitProvider: "github",
		GitRepo:     "acme/team-argocd-app",
	})
	require.NoError(t, err)

	tenantDir := filepath.Join(dir, "team-argocd")

	// RBAC + ArgoCD files must exist (argocd-rbac-cm.yaml is NOT generated per-tenant).
	for _, f := range []string{"namespace.yaml", "serviceaccount.yaml", "rolebinding.yaml", "project.yaml", "app.yaml", "kustomization.yaml"} {
		_, err := os.Stat(filepath.Join(tenantDir, f))
		require.NoError(t, err, "expected %s to exist", f)
	}

	// argocd-rbac-cm.yaml should NOT be generated per-tenant.
	_, err = os.Stat(filepath.Join(tenantDir, "argocd-rbac-cm.yaml"))
	require.ErrorIs(t, err, os.ErrNotExist, "argocd-rbac-cm.yaml should not be generated per-tenant")

	// Verify ArgoCD files are in kustomization resources.
	kustomizationContent, err := os.ReadFile(filepath.Join(tenantDir, "kustomization.yaml"))
	require.NoError(t, err)
	content := string(kustomizationContent)
	require.Contains(t, content, "project.yaml")
	require.Contains(t, content, "app.yaml")

	snaps.MatchSnapshot(t, content)
}

func TestGenerate_ForceOverwrite(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	opts := Options{
		Name:       "team-force",
		TenantType: TenantTypeKubectl,
		OutputDir:  dir,
	}

	// First run succeeds.
	require.NoError(t, Generate(opts))

	// Second run without force fails.
	err := Generate(opts)
	require.Error(t, err)
	require.Contains(t, err.Error(), "already exists")

	// Third run with force succeeds.
	opts.Force = true
	require.NoError(t, Generate(opts))

	// Verify files still exist after force overwrite.
	for _, f := range []string{"namespace.yaml", "serviceaccount.yaml", "rolebinding.yaml", "kustomization.yaml"} {
		_, err := os.Stat(filepath.Join(dir, "team-force", f))
		require.NoError(t, err, "expected %s to exist after force overwrite", f)
	}
}

func TestGenerate_EmptyTenantName(t *testing.T) {
	t.Parallel()

	err := Generate(Options{
		TenantType: TenantTypeKubectl,
		OutputDir:  t.TempDir(),
	})
	require.ErrorIs(t, err, ErrTenantNameRequired)
}

func TestGenerate_EmptyTenantType(t *testing.T) {
	t.Parallel()

	err := Generate(Options{
		Name:      "team-notype",
		OutputDir: t.TempDir(),
	})
	require.ErrorIs(t, err, ErrTenantTypeRequired)
}

func TestGenerate_ForceRemovesStalePreviousFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// First: create an ArgoCD tenant.
	require.NoError(t, Generate(Options{
		Name:        "team-switch",
		TenantType:  TenantTypeArgoCD,
		OutputDir:   dir,
		GitProvider: "github",
		GitRepo:     "acme/team-switch",
	}))

	tenantDir := filepath.Join(dir, "team-switch")
	_, err := os.Stat(filepath.Join(tenantDir, "project.yaml"))
	require.NoError(t, err, "ArgoCD project.yaml should exist initially")

	// Now force-regenerate as kubectl (should remove ArgoCD-specific files).
	require.NoError(t, Generate(Options{
		Name:       "team-switch",
		TenantType: TenantTypeKubectl,
		OutputDir:  dir,
		Force:      true,
	}))

	_, err = os.Stat(filepath.Join(tenantDir, "project.yaml"))
	require.ErrorIs(t, err, os.ErrNotExist, "project.yaml should be removed after force-switching to kubectl")
	_, err = os.Stat(filepath.Join(tenantDir, "app.yaml"))
	require.ErrorIs(t, err, os.ErrNotExist, "app.yaml should be removed after force-switching to kubectl")
}

func TestGenerate_FluxGitSource(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	err := Generate(Options{
		Name:        "team-flux-git",
		TenantType:  TenantTypeFlux,
		OutputDir:   dir,
		SyncSource:  SyncSourceGit,
		GitProvider: "github",
		GitRepo:     "acme/team-flux-git",
	})
	require.NoError(t, err)

	tenantDir := filepath.Join(dir, "team-flux-git")
	syncContent, err := os.ReadFile(filepath.Join(tenantDir, "sync.yaml"))
	require.NoError(t, err)
	require.Contains(t, string(syncContent), "kind: GitRepository")
	require.Contains(t, string(syncContent), "github.com")
}

func TestGenerate_MultiNamespace(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	err := Generate(Options{
		Name:       "team-multi",
		Namespaces: []string{"ns-dev", "ns-staging", "ns-prod"},
		TenantType: TenantTypeKubectl,
		OutputDir:  dir,
	})
	require.NoError(t, err)

	tenantDir := filepath.Join(dir, "team-multi")

	// Namespace and RoleBinding should have 3 documents (one per namespace).
	for _, f := range []string{"namespace.yaml", "rolebinding.yaml"} {
		content, err := os.ReadFile(filepath.Join(tenantDir, f))
		require.NoError(t, err)
		docs := strings.Split(string(content), "---\n")
		require.Len(t, docs, 3, "expected 3 documents in %s for 3 namespaces", f)
	}

	// ServiceAccount is single (only in primary namespace).
	saContent, err := os.ReadFile(filepath.Join(tenantDir, "serviceaccount.yaml"))
	require.NoError(t, err)
	saDocs := strings.Split(string(saContent), "---\n")
	require.Len(t, saDocs, 1, "expected 1 document in serviceaccount.yaml")
}
