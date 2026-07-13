package env_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/project/env"
	"github.com/devantler-tech/ksail/v7/pkg/cli/flags"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// reconcileBaseConfig is a minimal base ksail.yaml: the reconcile plan spans
// the whole workspace, so the handler reads the source directory from it.
const reconcileBaseConfig = `apiVersion: ksail.io/v1alpha1
kind: Cluster
metadata:
  name: base
spec:
  cluster:
    distribution: Vanilla
    provider: Docker
  workload:
    sourceDirectory: k8s
`

// writeReconcileRepo materialises a workspace with a base config, a declared
// "prod" environment WITH its overlay, and a declared "staging" environment
// WITHOUT one — the state a reconcile resolves.
func writeReconcileRepo(t *testing.T) string {
	t.Helper()

	repoRoot := t.TempDir()
	files := map[string]string{
		"ksail.yaml":                           reconcileBaseConfig,
		"ksail.prod.yaml":                      addEnvSourceConfig,
		"ksail.staging.yaml":                   addEnvSourceConfig,
		"k8s/clusters/prod/kustomization.yaml": addEnvOverlayKustomization,
	}

	for rel, content := range files {
		abs := filepath.Join(repoRoot, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(abs), 0o750))
		require.NoError(t, os.WriteFile(abs, []byte(content), 0o600))
	}

	return repoRoot
}

// runReconcile executes the reconcile command standalone — with the
// experimental gate satisfied, mirroring the rm precedent — and returns its
// combined output and error. The disabled state is covered by
// TestHandleReconcileRunE_ExperimentalDisabled.
func runReconcile(t *testing.T) (string, error) {
	t.Helper()

	cmd := env.NewReconcileCmd()
	cmd.Flags().Bool(flags.ExperimentalFlagName, true, "")

	var out bytes.Buffer

	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(nil)

	err := cmd.Execute()

	return out.String(), err
}

//nolint:paralleltest // uses t.Chdir to set the working directory
func TestHandleReconcileRunE_ExperimentalDisabled(t *testing.T) {
	repoRoot := writeReconcileRepo(t)
	t.Chdir(repoRoot)

	cmd := env.NewReconcileCmd()

	var out bytes.Buffer

	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "experimental")

	// The gate refused before anything was generated.
	_, statErr := os.Stat(filepath.Join(repoRoot, "k8s", "clusters", "staging"))
	require.ErrorIs(t, statErr, os.ErrNotExist)
}

//nolint:paralleltest // uses t.Chdir to set the working directory
func TestHandleReconcileRunE_GeneratesMissingOverlay(t *testing.T) {
	repoRoot := writeReconcileRepo(t)
	t.Chdir(repoRoot)

	out, err := runReconcile(t)
	require.NoError(t, err)

	// The plan names both declared environments with their states.
	assert.Contains(t, out, "prod")
	assert.Contains(t, out, "Present")
	assert.Contains(t, out, "staging")
	assert.Contains(t, out, "Missing")

	// The missing overlay (and the shared base it references) were scaffolded.
	_, statErr := os.Stat(
		filepath.Join(repoRoot, "k8s", "clusters", "staging", "kustomization.yaml"),
	)
	require.NoError(t, statErr)
	_, statErr = os.Stat(
		filepath.Join(repoRoot, "k8s", "clusters", "base", "kustomization.yaml"),
	)
	require.NoError(t, statErr)

	assert.Contains(t, out, "reconciled 1 missing environment overlay")
}

//nolint:paralleltest // uses t.Chdir to set the working directory
func TestHandleReconcileRunE_SecondRunRewritesNothing(t *testing.T) {
	repoRoot := writeReconcileRepo(t)
	t.Chdir(repoRoot)

	_, err := runReconcile(t)
	require.NoError(t, err)

	generated := filepath.Join(repoRoot, "k8s", "clusters", "staging", "kustomization.yaml")
	marker := []byte("# operator-edited\n")
	require.NoError(t, os.WriteFile(generated, marker, 0o600))

	_, err = runReconcile(t)
	require.NoError(t, err)

	// Force is hardwired off: the operator's edit survives the second run.
	content, readErr := os.ReadFile(generated) //nolint:gosec // test's own TempDir
	require.NoError(t, readErr)
	assert.Equal(t, marker, content)
}

//nolint:paralleltest // uses t.Chdir to set the working directory
func TestHandleReconcileRunE_SurfacesOrphansWithoutDeleting(t *testing.T) {
	repoRoot := writeReconcileRepo(t)
	orphan := filepath.Join(repoRoot, "k8s", "clusters", "legacy")
	require.NoError(t, os.MkdirAll(orphan, 0o750))
	t.Chdir(repoRoot)

	out, err := runReconcile(t)
	require.NoError(t, err)

	assert.Contains(t, out, "orphan overlay clusters/legacy")

	// Orphans are surfaced, never deleted.
	_, statErr := os.Stat(orphan)
	require.NoError(t, statErr)
}

//nolint:paralleltest // uses t.Chdir to set the working directory
func TestHandleReconcileRunE_NothingMissing(t *testing.T) {
	repoRoot := writeReconcileRepo(t)
	require.NoError(t, os.MkdirAll(
		filepath.Join(repoRoot, "k8s", "clusters", "staging"), 0o750,
	))
	t.Chdir(repoRoot)

	out, err := runReconcile(t)
	require.NoError(t, err)

	assert.Contains(t, out, "nothing to generate")
}

//nolint:paralleltest // uses t.Chdir to set the working directory
func TestHandleReconcileRunE_MissingBaseConfig(t *testing.T) {
	repoRoot := t.TempDir()
	t.Chdir(repoRoot)

	_, err := runReconcile(t)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "workspace base config")
}

func TestNewReconcileCmd_Structure(t *testing.T) {
	t.Parallel()

	cmd := env.NewReconcileCmd()

	assert.Equal(t, "reconcile", cmd.Use)
	assert.Equal(
		t, "write", cmd.Annotations[annotations.AnnotationPermission],
	)
}
