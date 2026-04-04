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

	// RBAC + ArgoCD files must exist.
	for _, f := range []string{"namespace.yaml", "serviceaccount.yaml", "rolebinding.yaml", "project.yaml", "app.yaml", "argocd-rbac-cm.yaml", "kustomization.yaml"} {
		_, err := os.Stat(filepath.Join(tenantDir, f))
		require.NoError(t, err, "expected %s to exist", f)
	}

	// Verify ArgoCD files are in kustomization resources.
	kustomizationContent, err := os.ReadFile(filepath.Join(tenantDir, "kustomization.yaml"))
	require.NoError(t, err)
	content := string(kustomizationContent)
	require.Contains(t, content, "project.yaml")
	require.Contains(t, content, "app.yaml")
	require.Contains(t, content, "argocd-rbac-cm.yaml")

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

	// Each RBAC file should contain multiple YAML documents.
	for _, f := range []string{"namespace.yaml", "serviceaccount.yaml", "rolebinding.yaml"} {
		content, err := os.ReadFile(filepath.Join(tenantDir, f))
		require.NoError(t, err)
		docs := strings.Split(string(content), "---\n")
		require.Len(t, docs, 3, "expected 3 documents in %s for 3 namespaces", f)
	}
}
