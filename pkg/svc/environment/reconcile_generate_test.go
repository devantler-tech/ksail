package environment_test

import (
	"os"
	"path/filepath"
	"testing"

	kustomizationgenerator "github.com/devantler-tech/ksail/v7/pkg/fsutil/generator/kustomization"
	"github.com/devantler-tech/ksail/v7/pkg/svc/environment"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// populatedBase is a hand-authored shared base kustomization whose bytes the
// generation step must preserve (force is hardwired off).
const populatedBase = "resources:\n  - podinfo.yaml\n"

// writeBaseKustomization seeds <dir>/<sourceDir>/clusters/base/kustomization.yaml
// with the populated base content.
func writeBaseKustomization(t *testing.T, dir string) string {
	t.Helper()

	basePath := filepath.Join(
		dir, sourceDir, environment.ClustersDir, environment.BaseEnvName, "kustomization.yaml",
	)
	require.NoError(t, os.MkdirAll(filepath.Dir(basePath), 0o750))
	require.NoError(t, os.WriteFile(basePath, []byte(populatedBase), 0o600))

	return basePath
}

// generatePlan derives the plan for dir and runs GenerateMissingOverlays over it
// with the real kustomization generator, returning the plan and written paths.
func generatePlan(t *testing.T, dir string) (environment.Plan, []string) {
	t.Helper()

	plan, err := environment.DerivePlan(dir, sourceDir, planLoader())
	require.NoError(t, err)

	written, err := environment.GenerateMissingOverlays(
		kustomizationgenerator.NewGenerator(), filepath.Join(dir, sourceDir), plan,
	)
	require.NoError(t, err)

	return plan, written
}

func TestGenerateMissingOverlaysScaffoldsMissingEntryPreservingBase(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFiles(t, dir, "ksail.yaml", "ksail.local.yaml", "ksail.prod.yaml")
	mkOverlays(t, dir, "prod")
	basePath := writeBaseKustomization(t, dir)

	_, written := generatePlan(t, dir)

	overlayPath := filepath.Join(dir, sourceDir, "clusters", "local", "kustomization.yaml")
	assert.Equal(t, []string{basePath, overlayPath}, written)
	assert.Equal(t, populatedBase, readFile(t, basePath))
	assert.Contains(t, readFile(t, overlayPath), "../base")
	// The present overlay is untouched — generation only resolves Missing entries.
	assert.NoFileExists(t, filepath.Join(dir, sourceDir, "clusters", "prod", "kustomization.yaml"))
}

func TestGenerateMissingOverlaysScaffoldsPreScaffoldTreeWithSharedBaseOnce(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFiles(t, dir, "ksail.local.yaml", "ksail.prod.yaml")

	_, written := generatePlan(t, dir)

	clusters := filepath.Join(dir, sourceDir, "clusters")
	assert.Equal(t, []string{
		filepath.Join(clusters, "base", "kustomization.yaml"),
		filepath.Join(clusters, "local", "kustomization.yaml"),
		filepath.Join(clusters, "prod", "kustomization.yaml"),
	}, written)

	for _, env := range []string{"local", "prod"} {
		overlay := readFile(t, filepath.Join(clusters, env, "kustomization.yaml"))
		assert.Contains(t, overlay, "../base")
	}
}

func TestGenerateMissingOverlaysIsANoOpForPresentEntriesAndOrphans(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFiles(t, dir, "ksail.local.yaml", "ksail.prod.yaml")
	mkOverlays(t, dir, "local", "prod", "attic")

	plan, written := generatePlan(t, dir)

	assert.Empty(t, written)
	assert.Equal(t, []string{"clusters/attic"}, plan.Orphans)
	// The orphan overlay is surfaced, never scaffolded into or deleted.
	assert.NoFileExists(t, filepath.Join(dir, sourceDir, "clusters", "attic", "kustomization.yaml"))
}

func TestGenerateMissingOverlaysIsIdempotent(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFiles(t, dir, "ksail.local.yaml", "ksail.prod.yaml")

	_, first := generatePlan(t, dir)

	contents := make(map[string]string, len(first))
	for _, path := range first {
		contents[path] = readFile(t, path)
	}

	// Re-deriving reports every overlay Present; a second generation over the
	// original plan (all Missing) must also rewrite nothing (force is off).
	plan, err := environment.DerivePlan(dir, sourceDir, planLoader())
	require.NoError(t, err)

	for _, entry := range plan.Entries {
		assert.Equal(t, environment.OverlayPresent, entry.State)
	}

	_, second := generatePlan(t, dir)
	assert.Empty(t, second)

	for path, content := range contents {
		assert.Equal(t, content, readFile(t, path))
	}
}

func TestGenerateMissingOverlaysRejectsReservedBaseName(t *testing.T) {
	t.Parallel()

	plan := environment.Plan{
		Entries: []environment.PlanEntry{{
			Environment: environment.Environment{Name: environment.BaseEnvName},
			OverlayDir:  "clusters/base",
			State:       environment.OverlayMissing,
		}},
	}

	_, err := environment.GenerateMissingOverlays(
		kustomizationgenerator.NewGenerator(), t.TempDir(), plan,
	)

	require.ErrorIs(t, err, environment.ErrReservedEnvironmentName)
}
