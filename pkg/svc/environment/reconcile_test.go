package environment_test

import (
	"os"
	"path/filepath"
	"testing"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/svc/environment"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sourceDir mirrors the conventional GitOps source directory the plan diffs
// against in these tests.
const sourceDir = "k8s"

// mkOverlays creates <dir>/<sourceDir>/clusters/<name> directories.
func mkOverlays(t *testing.T, dir string, names ...string) {
	t.Helper()

	for _, name := range names {
		require.NoError(
			t,
			os.MkdirAll(filepath.Join(dir, sourceDir, environment.ClustersDir, name), 0o750),
		)
	}
}

func planLoader() environment.ConfigLoader {
	return stubLoader(map[string]*v1alpha1.Cluster{
		"ksail.local.yaml": clusterConfig(v1alpha1.DistributionVanilla, v1alpha1.ProviderDocker),
		"ksail.prod.yaml":  clusterConfig(v1alpha1.DistributionTalos, v1alpha1.ProviderHetzner),
	})
}

func TestDerivePlanReportsPresentAndMissingOverlays(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFiles(t, dir, "ksail.yaml", "ksail.local.yaml", "ksail.prod.yaml")
	mkOverlays(t, dir, "prod")

	plan, err := environment.DerivePlan(dir, sourceDir, planLoader())

	require.NoError(t, err)
	require.Len(t, plan.Entries, 2)
	assert.Equal(t, "local", plan.Entries[0].Environment.Name)
	assert.Equal(t, "clusters/local", plan.Entries[0].OverlayDir)
	assert.Equal(t, environment.OverlayMissing, plan.Entries[0].State)
	assert.Equal(t, "prod", plan.Entries[1].Environment.Name)
	assert.Equal(t, "clusters/prod", plan.Entries[1].OverlayDir)
	assert.Equal(t, environment.OverlayPresent, plan.Entries[1].State)
	assert.Empty(t, plan.Orphans)
}

func TestDerivePlanReportsOrphanOverlaysExcludingBase(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFiles(t, dir, "ksail.prod.yaml")
	mkOverlays(t, dir, "prod", "base", "staging", "attic")

	plan, err := environment.DerivePlan(dir, sourceDir, planLoader())

	require.NoError(t, err)
	require.Len(t, plan.Entries, 1)
	assert.Equal(t, environment.OverlayPresent, plan.Entries[0].State)
	assert.Equal(t, []string{"clusters/attic", "clusters/staging"}, plan.Orphans)
}

func TestDerivePlanTreatsMissingClustersTreeAsPreScaffold(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFiles(t, dir, "ksail.local.yaml", "ksail.prod.yaml")

	plan, err := environment.DerivePlan(dir, sourceDir, planLoader())

	require.NoError(t, err)
	require.Len(t, plan.Entries, 2)

	for _, entry := range plan.Entries {
		assert.Equal(t, environment.OverlayMissing, entry.State)
	}

	assert.Empty(t, plan.Orphans)
}

func TestDerivePlanIgnoresFilesInClustersTree(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFiles(t, dir, "ksail.prod.yaml")
	mkOverlays(t, dir, "prod")
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, sourceDir, environment.ClustersDir, "README.md"),
		[]byte("not an overlay\n"),
		0o600,
	))

	plan, err := environment.DerivePlan(dir, sourceDir, planLoader())

	require.NoError(t, err)
	assert.Empty(t, plan.Orphans)
}

func TestDerivePlanEmptyWorkspaceYieldsEmptyPlan(t *testing.T) {
	t.Parallel()

	plan, err := environment.DerivePlan(t.TempDir(), sourceDir, planLoader())

	require.NoError(t, err)
	assert.Empty(t, plan.Entries)
	assert.Empty(t, plan.Orphans)
}

func TestDerivePlanErrorsOnUnreadableWorkspace(t *testing.T) {
	t.Parallel()

	_, err := environment.DerivePlan(
		filepath.Join(t.TempDir(), "does-not-exist"),
		sourceDir,
		planLoader(),
	)

	require.ErrorIs(t, err, environment.ErrDiscoverEnvironments)
}

func TestDerivePlanErrorsOnUnreadableClustersTree(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFiles(t, dir, "ksail.prod.yaml")
	// A regular FILE at the clusters/ path makes ReadDir fail with ENOTDIR —
	// distinct from the valid missing-directory pre-scaffold state.
	require.NoError(t, os.MkdirAll(filepath.Join(dir, sourceDir), 0o750))
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, sourceDir, environment.ClustersDir),
		[]byte("a file, not a directory\n"),
		0o600,
	))

	_, err := environment.DerivePlan(dir, sourceDir, planLoader())

	require.ErrorIs(t, err, environment.ErrDerivePlan)
}
