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

// canonicalTempDir returns t.TempDir() with symlinked ancestors resolved
// (macOS's /var → /private/var): generation canonicalizes sourceDir
// ancestors, so tests asserting absolute output paths need the physical path.
func canonicalTempDir(t *testing.T) string {
	t.Helper()

	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)

	return dir
}

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

	dir := canonicalTempDir(t)
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

	dir := canonicalTempDir(t)
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

	dir := canonicalTempDir(t)
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

	dir := canonicalTempDir(t)
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

// symlinkOrSkip creates a symlink or skips the test on platforms where
// symlink creation is not permitted (e.g. Windows without extra privileges).
func symlinkOrSkip(t *testing.T, target, link string) {
	t.Helper()

	err := os.Symlink(target, link)
	if err != nil {
		t.Skipf("symlinks not supported: %v", err)
	}
}

// generateExpectingUnsafePath derives the plan for dir and asserts generation
// refuses it with ErrUnsafeOverlayPath.
func generateExpectingUnsafePath(t *testing.T, dir string) {
	t.Helper()

	plan, err := environment.DerivePlan(dir, sourceDir, planLoader())
	require.NoError(t, err)

	_, err = environment.GenerateMissingOverlays(
		kustomizationgenerator.NewGenerator(), filepath.Join(dir, sourceDir), plan,
	)

	require.ErrorIs(t, err, environment.ErrUnsafeOverlayPath)
}

func TestGenerateMissingOverlaysRejectsSymlinkedOverlayDir(t *testing.T) {
	t.Parallel()

	dir := canonicalTempDir(t)
	writeFiles(t, dir, "ksail.local.yaml", "ksail.prod.yaml")
	mkOverlays(t, dir, "prod")

	// DerivePlan reports a symlinked clusters/local as Missing (only real
	// directories count), but writing through it would land in the target.
	target := filepath.Join(dir, "outside-target")
	require.NoError(t, os.MkdirAll(target, 0o750))
	symlinkOrSkip(t, target, filepath.Join(dir, sourceDir, environment.ClustersDir, "local"))

	generateExpectingUnsafePath(t, dir)
	assert.NoFileExists(t, filepath.Join(target, "kustomization.yaml"))
}

func TestGenerateMissingOverlaysRejectsSymlinkedClustersRoot(t *testing.T) {
	t.Parallel()

	dir := canonicalTempDir(t)
	writeFiles(t, dir, "ksail.local.yaml", "ksail.prod.yaml")

	target := filepath.Join(dir, "outside-clusters")
	require.NoError(t, os.MkdirAll(target, 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, sourceDir), 0o750))
	symlinkOrSkip(t, target, filepath.Join(dir, sourceDir, environment.ClustersDir))

	generateExpectingUnsafePath(t, dir)
	assert.NoDirExists(t, filepath.Join(target, "base"))
}

func TestGenerateMissingOverlaysRejectsSymlinkedLayoutFile(t *testing.T) {
	t.Parallel()

	dir := canonicalTempDir(t)
	writeFiles(t, dir, "ksail.local.yaml", "ksail.prod.yaml")
	mkOverlays(t, dir, environment.BaseEnvName)

	// A dangling symlink at clusters/base/kustomization.yaml Lstats as a
	// symlink but Stats as absent, so a force-off write would follow it.
	escape := filepath.Join(dir, "escape-file.yaml")
	symlinkOrSkip(t, escape, filepath.Join(
		dir, sourceDir, environment.ClustersDir, environment.BaseEnvName, "kustomization.yaml",
	))

	generateExpectingUnsafePath(t, dir)
	assert.NoFileExists(t, escape)
}

func TestGenerateMissingOverlaysRejectsSymlinkedSourceDir(t *testing.T) {
	t.Parallel()

	dir := canonicalTempDir(t)
	writeFiles(t, dir, "ksail.local.yaml", "ksail.prod.yaml")

	// The per-file walk starts below sourceDir, so a symlinked source root
	// must be rejected up front — every write would land in the link target.
	target := filepath.Join(dir, "outside-source")
	require.NoError(t, os.MkdirAll(target, 0o750))
	symlinkOrSkip(t, target, filepath.Join(dir, sourceDir))

	generateExpectingUnsafePath(t, dir)
	assert.NoDirExists(t, filepath.Join(target, environment.ClustersDir))
}

func TestGenerateMissingOverlaysResolvesSymlinkedAncestorOfSourceDir(t *testing.T) {
	t.Parallel()

	dir := canonicalTempDir(t)

	// A symlinked ANCESTOR of sourceDir (macOS's /var → /private/var class) is
	// canonicalized up front, not rejected and not blindly followed at write
	// time: generation succeeds and the layout lands in the physical target.
	physical := filepath.Join(dir, "physical")
	require.NoError(t, os.MkdirAll(physical, 0o750))

	link := filepath.Join(dir, "link")
	symlinkOrSkip(t, physical, link)

	writeFiles(t, dir, "ksail.local.yaml")

	plan, err := environment.DerivePlan(dir, sourceDir, planLoader())
	require.NoError(t, err)

	written, err := environment.GenerateMissingOverlays(
		kustomizationgenerator.NewGenerator(), filepath.Join(link, sourceDir), plan,
	)
	require.NoError(t, err)
	require.NotEmpty(t, written)

	assert.FileExists(t, filepath.Join(
		physical, sourceDir, environment.ClustersDir, environment.BaseEnvName, "kustomization.yaml",
	))
}

func TestGenerateMissingOverlaysNormalizesTrailingSeparator(t *testing.T) {
	t.Parallel()

	dir := canonicalTempDir(t)
	writeFiles(t, dir, "ksail.local.yaml")

	plan, err := environment.DerivePlan(dir, sourceDir, planLoader())
	require.NoError(t, err)

	// A trailing separator must not make Dir/Base duplicate the leaf
	// (`/repo/k8s/` once resolved as `/repo/k8s/k8s`).
	trailing := filepath.Join(dir, sourceDir) + string(filepath.Separator)
	written, err := environment.GenerateMissingOverlays(
		kustomizationgenerator.NewGenerator(), trailing, plan,
	)
	require.NoError(t, err)
	require.NotEmpty(t, written)

	assert.FileExists(t, filepath.Join(
		dir, sourceDir, environment.ClustersDir, environment.BaseEnvName, "kustomization.yaml",
	))
	assert.NoDirExists(t, filepath.Join(dir, sourceDir, sourceDir))
}

func TestGenerateMissingOverlaysExpandsHomePrefixedSourceDir(t *testing.T) {
	home := canonicalTempDir(t)
	t.Setenv("HOME", home)

	dir := canonicalTempDir(t)
	writeFiles(t, dir, "ksail.local.yaml")

	plan, err := environment.DerivePlan(dir, sourceDir, planLoader())
	require.NoError(t, err)

	// `~/` must resolve to the user's home, never to a literal `./~/` dir.
	written, err := environment.GenerateMissingOverlays(
		kustomizationgenerator.NewGenerator(), "~/"+sourceDir, plan,
	)
	require.NoError(t, err)
	require.NotEmpty(t, written)

	assert.FileExists(t, filepath.Join(
		home, sourceDir, environment.ClustersDir, environment.BaseEnvName, "kustomization.yaml",
	))
	assert.NoDirExists(t, "~")
}

func TestGenerateMissingOverlaysRejectsDirectoryLayoutLeaf(t *testing.T) {
	t.Parallel()

	dir := canonicalTempDir(t)
	writeFiles(t, dir, "ksail.local.yaml", "ksail.prod.yaml")

	// A directory at clusters/base/kustomization.yaml Stats as existing, so
	// the force-off writer would skip the file yet report it as resolved,
	// leaving overlays that reference a base without a kustomization.
	require.NoError(t, os.MkdirAll(
		filepath.Join(
			dir, sourceDir, environment.ClustersDir, environment.BaseEnvName, "kustomization.yaml",
		),
		0o750,
	))

	generateExpectingUnsafePath(t, dir)
}
