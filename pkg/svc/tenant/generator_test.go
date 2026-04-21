package tenant_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/tenant"
	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/stretchr/testify/require"
)

func TestGenerate_KubectlType(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	err := tenant.Generate(tenant.Options{
		Name:       "team-kubectl",
		TenantType: tenant.TypeKubectl,
		OutputDir:  dir,
	})
	require.NoError(t, err)

	tenantDir := filepath.Join(dir, "team-kubectl")

	// RBAC files must exist.
	rbacFiles := []string{
		"namespace.yaml", "serviceaccount.yaml",
		"rolebinding.yaml", "kustomization.yaml",
	}
	for _, f := range rbacFiles {
		_, err := os.Stat(filepath.Join(tenantDir, f))
		require.NoError(t, err, "expected %s to exist", f)
	}

	// Type-specific files must NOT exist.
	typeFiles := []string{
		"sync.yaml", "project.yaml",
		"app.yaml", "argocd-rbac-cm.yaml",
	}
	for _, f := range typeFiles {
		_, err := os.Stat(filepath.Join(tenantDir, f))
		require.ErrorIs(t, err, os.ErrNotExist,
			"expected %s to not exist", f)
	}

	// Snapshot the kustomization.yaml.
	kPath := filepath.Join(tenantDir, "kustomization.yaml")
	kustomizationContent, err := os.ReadFile(kPath) //nolint:gosec // test path
	require.NoError(t, err)
	snaps.MatchSnapshot(t, string(kustomizationContent))
}

func TestGenerate_FluxType(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	err := tenant.Generate(tenant.Options{
		Name:       "team-flux",
		TenantType: tenant.TypeFlux,
		OutputDir:  dir,
		SyncSource: tenant.SyncSourceOCI,
		Registry:   "oci://ghcr.io",
		TenantRepo: "acme/team-flux-manifests",
	})
	require.NoError(t, err)

	tenantDir := filepath.Join(dir, "team-flux")

	// RBAC + Flux files must exist.
	fluxFiles := []string{
		"namespace.yaml", "serviceaccount.yaml",
		"rolebinding.yaml", "sync.yaml", "kustomization.yaml",
	}
	for _, f := range fluxFiles {
		_, err := os.Stat(filepath.Join(tenantDir, f))
		require.NoError(t, err, "expected %s to exist", f)
	}

	// Verify sync.yaml is in kustomization resources.
	kPath := filepath.Join(tenantDir, "kustomization.yaml")
	kustomizationContent, err := os.ReadFile(kPath) //nolint:gosec // test path
	require.NoError(t, err)
	require.Contains(t, string(kustomizationContent), "sync.yaml")

	snaps.MatchSnapshot(t, string(kustomizationContent))
}

func TestGenerate_ArgoCDType(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	err := tenant.Generate(tenant.Options{
		Name:        "team-argocd",
		TenantType:  tenant.TypeArgoCD,
		OutputDir:   dir,
		GitProvider: "github",
		TenantRepo:  "acme/team-argocd-app",
	})
	require.NoError(t, err)

	tenantDir := filepath.Join(dir, "team-argocd")

	// RBAC + ArgoCD files must exist.
	argoFiles := []string{
		"namespace.yaml", "serviceaccount.yaml",
		"rolebinding.yaml", "project.yaml",
		"app.yaml", "kustomization.yaml",
	}
	for _, f := range argoFiles {
		_, err := os.Stat(filepath.Join(tenantDir, f))
		require.NoError(t, err, "expected %s to exist", f)
	}

	// argocd-rbac-cm.yaml should NOT be generated per-tenant.
	_, err = os.Stat(filepath.Join(tenantDir, "argocd-rbac-cm.yaml"))
	require.ErrorIs(t, err, os.ErrNotExist,
		"argocd-rbac-cm.yaml should not be generated per-tenant")

	// Verify ArgoCD files are in kustomization resources.
	kPath := filepath.Join(tenantDir, "kustomization.yaml")
	kustomizationContent, err := os.ReadFile(kPath) //nolint:gosec // test path
	require.NoError(t, err)

	content := string(kustomizationContent)
	require.Contains(t, content, "project.yaml")
	require.Contains(t, content, "app.yaml")

	snaps.MatchSnapshot(t, content)
}

func TestGenerate_ForceOverwrite(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	opts := tenant.Options{
		Name:       "team-force",
		TenantType: tenant.TypeKubectl,
		OutputDir:  dir,
	}

	// First run succeeds.
	require.NoError(t, tenant.Generate(opts))

	// Second run without force fails.
	err := tenant.Generate(opts)
	require.Error(t, err)
	require.Contains(t, err.Error(), "already exists")

	// Third run with force succeeds.
	opts.Force = true
	require.NoError(t, tenant.Generate(opts))

	// Verify files still exist after force overwrite.
	forceFiles := []string{
		"namespace.yaml", "serviceaccount.yaml",
		"rolebinding.yaml", "kustomization.yaml",
	}
	for _, f := range forceFiles {
		_, err := os.Stat(filepath.Join(dir, "team-force", f))
		require.NoError(t, err,
			"expected %s to exist after force overwrite", f)
	}
}

func TestGenerate_EmptyTenantName(t *testing.T) {
	t.Parallel()

	err := tenant.Generate(tenant.Options{
		TenantType: tenant.TypeKubectl,
		OutputDir:  t.TempDir(),
	})
	require.ErrorIs(t, err, tenant.ErrTenantNameRequired)
}

func TestGenerate_EmptyTenantType(t *testing.T) {
	t.Parallel()

	err := tenant.Generate(tenant.Options{
		Name:      "team-notype",
		OutputDir: t.TempDir(),
	})
	require.ErrorIs(t, err, tenant.ErrTenantTypeRequired)
}

func TestGenerate_ForceRemovesStalePreviousFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// First: create an ArgoCD tenant.
	require.NoError(t, tenant.Generate(tenant.Options{
		Name:        "team-switch",
		TenantType:  tenant.TypeArgoCD,
		OutputDir:   dir,
		GitProvider: "github",
		TenantRepo:  "acme/team-switch",
	}))

	tenantDir := filepath.Join(dir, "team-switch")
	_, err := os.Stat(filepath.Join(tenantDir, "project.yaml"))
	require.NoError(t, err,
		"ArgoCD project.yaml should exist initially")

	// Now force-regenerate as kubectl.
	require.NoError(t, tenant.Generate(tenant.Options{
		Name:       "team-switch",
		TenantType: tenant.TypeKubectl,
		OutputDir:  dir,
		Force:      true,
	}))

	_, err = os.Stat(filepath.Join(tenantDir, "project.yaml"))
	require.ErrorIs(t, err, os.ErrNotExist,
		"project.yaml should be removed after force-switching to kubectl")
	_, err = os.Stat(filepath.Join(tenantDir, "app.yaml"))
	require.ErrorIs(t, err, os.ErrNotExist,
		"app.yaml should be removed after force-switching to kubectl")
}

func TestGenerate_FluxGitSource(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	err := tenant.Generate(tenant.Options{
		Name:        "team-flux-git",
		TenantType:  tenant.TypeFlux,
		OutputDir:   dir,
		SyncSource:  tenant.SyncSourceGit,
		GitProvider: "github",
		TenantRepo:  "acme/team-flux-git",
	})
	require.NoError(t, err)

	tenantDir := filepath.Join(dir, "team-flux-git")
	syncPath := filepath.Join(tenantDir, "sync.yaml")
	syncContent, err := os.ReadFile(syncPath) //nolint:gosec // test path
	require.NoError(t, err)
	require.Contains(t, string(syncContent), "kind: GitRepository")
	require.Contains(t, string(syncContent), "github.com")
}

func TestGenerate_MultiNamespace(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	err := tenant.Generate(tenant.Options{
		Name:       "team-multi",
		Namespaces: []string{"ns-dev", "ns-staging", "ns-prod"},
		TenantType: tenant.TypeKubectl,
		OutputDir:  dir,
	})
	require.NoError(t, err)

	tenantDir := filepath.Join(dir, "team-multi")

	// Namespace and RoleBinding: 3 docs (one per namespace).
	for _, fileName := range []string{"namespace.yaml", "rolebinding.yaml"} {
		fPath := filepath.Join(tenantDir, fileName)
		content, err := os.ReadFile(fPath) //nolint:gosec // test path
		require.NoError(t, err)

		docs := strings.Split(string(content), "---\n")
		require.Len(t, docs, 3,
			"expected 3 documents in %s for 3 namespaces", fileName)
	}

	// ServiceAccount is single (only in primary namespace).
	saPath := filepath.Join(tenantDir, "serviceaccount.yaml")
	saContent, err := os.ReadFile(saPath) //nolint:gosec // test path
	require.NoError(t, err)

	saDocs := strings.Split(string(saContent), "---\n")
	require.Len(t, saDocs, 1,
		"expected 1 document in serviceaccount.yaml")
}
