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

// localOverlayDir is the overlay directory the base config's kustomizationFile
// points at in the base-sync test cases.
const localOverlayDir = "clusters/local"

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
	assert.Equal(t, localOverlayDir, plan.Entries[0].OverlayDir)
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

func TestDerivePlanAcceptsAbsoluteSourceDirectory(t *testing.T) {
	t.Parallel()

	// The ksail config manager absolutizes Spec.Workload.SourceDirectory before
	// handing it downstream; joining that against repoRoot again would probe
	// <repo>/<repo>/k8s/clusters and misread every overlay as Missing.
	dir := t.TempDir()
	writeFiles(t, dir, "ksail.prod.yaml")
	mkOverlays(t, dir, "prod", "attic")

	plan, err := environment.DerivePlan(dir, filepath.Join(dir, sourceDir), planLoader())

	require.NoError(t, err)
	require.Len(t, plan.Entries, 1)
	assert.Equal(t, environment.OverlayPresent, plan.Entries[0].State)
	assert.Equal(t, []string{"clusters/attic"}, plan.Orphans)
}

func TestDerivePlanRejectsReservedBaseEnvironment(t *testing.T) {
	t.Parallel()

	// ksail.base.yaml would map onto the SHARED clusters/base overlay; the plan
	// must surface the name collision DeriveMultiClusterLayout reserves instead
	// of reporting the shared base as that environment's Present overlay.
	dir := t.TempDir()
	writeFiles(t, dir, "ksail.base.yaml", "ksail.prod.yaml")
	mkOverlays(t, dir, "base", "prod")

	loader := stubLoader(map[string]*v1alpha1.Cluster{
		"ksail.base.yaml": clusterConfig(v1alpha1.DistributionVanilla, v1alpha1.ProviderDocker),
		"ksail.prod.yaml": clusterConfig(v1alpha1.DistributionTalos, v1alpha1.ProviderHetzner),
	})

	_, err := environment.DerivePlan(dir, sourceDir, loader)

	require.ErrorIs(t, err, environment.ErrReservedEnvironmentName)
}

func TestDerivePlanKeepsBaseConfigSyncedOverlayOutOfOrphans(t *testing.T) {
	t.Parallel()

	// `project init --multi-cluster local` scaffolds ksail.yaml with
	// kustomizationFile clusters/local and NO ksail.local.yaml: that initial
	// overlay is declared by the base config, not an orphan.
	dir := t.TempDir()
	writeFiles(t, dir, "ksail.yaml", "ksail.prod.yaml")
	mkOverlays(t, dir, "local", "prod", "attic")

	base := clusterConfig(v1alpha1.DistributionVanilla, v1alpha1.ProviderDocker)
	base.Spec.Workload.KustomizationFile = localOverlayDir

	loader := stubLoader(map[string]*v1alpha1.Cluster{
		"ksail.yaml":      base,
		"ksail.prod.yaml": clusterConfig(v1alpha1.DistributionTalos, v1alpha1.ProviderHetzner),
	})

	plan, err := environment.DerivePlan(dir, sourceDir, loader)

	require.NoError(t, err)
	// The base-synced environment is a first-class entry (ConfigFile
	// ksail.yaml), not merely excluded from the orphans.
	require.Len(t, plan.Entries, 2)
	assert.Equal(t, "local", plan.Entries[0].Environment.Name)
	assert.Equal(t, "ksail.yaml", plan.Entries[0].Environment.ConfigFile)
	assert.Equal(t, environment.OverlayPresent, plan.Entries[0].State)
	assert.Equal(t, "prod", plan.Entries[1].Environment.Name)
	assert.Equal(t, []string{"clusters/attic"}, plan.Orphans)
}

func TestDerivePlanReportsBaseSyncedOverlayMissing(t *testing.T) {
	t.Parallel()

	// The init-scaffolded initial environment has no ksail.<env>.yaml; when its
	// clusters/<env> overlay was deleted, the plan must report it Missing so a
	// reconcile can regenerate it — not silently omit it.
	dir := t.TempDir()
	writeFiles(t, dir, "ksail.yaml")
	mkOverlays(t, dir, "attic")

	base := clusterConfig(v1alpha1.DistributionVanilla, v1alpha1.ProviderDocker)
	base.Spec.Workload.KustomizationFile = localOverlayDir

	loader := stubLoader(map[string]*v1alpha1.Cluster{"ksail.yaml": base})

	plan, err := environment.DerivePlan(dir, sourceDir, loader)

	require.NoError(t, err)
	require.Len(t, plan.Entries, 1)
	assert.Equal(t, "local", plan.Entries[0].Environment.Name)
	assert.Equal(t, "ksail.yaml", plan.Entries[0].Environment.ConfigFile)
	assert.Equal(t, environment.OverlayMissing, plan.Entries[0].State)
	assert.Equal(t, []string{"clusters/attic"}, plan.Orphans)
}

func TestDerivePlanPrefersDeclaredConfigOverBaseSync(t *testing.T) {
	t.Parallel()

	// When ksail.local.yaml exists AND the base config syncs clusters/local,
	// the dedicated config's entry wins — no duplicate.
	dir := t.TempDir()
	writeFiles(t, dir, "ksail.yaml", "ksail.local.yaml")
	mkOverlays(t, dir, "local")

	base := clusterConfig(v1alpha1.DistributionVanilla, v1alpha1.ProviderDocker)
	base.Spec.Workload.KustomizationFile = localOverlayDir

	loader := stubLoader(map[string]*v1alpha1.Cluster{
		"ksail.yaml":       base,
		"ksail.local.yaml": clusterConfig(v1alpha1.DistributionTalos, v1alpha1.ProviderHetzner),
	})

	plan, err := environment.DerivePlan(dir, sourceDir, loader)

	require.NoError(t, err)
	require.Len(t, plan.Entries, 1)
	assert.Equal(t, "ksail.local.yaml", plan.Entries[0].Environment.ConfigFile)
	assert.Empty(t, plan.Orphans)
}

func TestDerivePlanIgnoresBaseConfigSyncPathsOutsideClusters(t *testing.T) {
	t.Parallel()

	// A base config syncing something that is not exactly clusters/<name>
	// (deeper path, empty, or elsewhere) declares no initial environment.
	dir := t.TempDir()
	writeFiles(t, dir, "ksail.yaml")
	mkOverlays(t, dir, "attic")

	base := clusterConfig(v1alpha1.DistributionVanilla, v1alpha1.ProviderDocker)
	base.Spec.Workload.KustomizationFile = "clusters/local/deeper"

	loader := stubLoader(map[string]*v1alpha1.Cluster{"ksail.yaml": base})

	plan, err := environment.DerivePlan(dir, sourceDir, loader)

	require.NoError(t, err)
	assert.Empty(t, plan.Entries)
	assert.Equal(t, []string{"clusters/attic"}, plan.Orphans)
}
