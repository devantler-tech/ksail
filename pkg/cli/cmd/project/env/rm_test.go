package env_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/project/env"
	"github.com/devantler-tech/ksail/v7/pkg/cli/flags"
	"github.com/devantler-tech/ksail/v7/pkg/svc/environment"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runRm executes the rm command standalone with args — with the experimental
// gate satisfied, mirroring the intercept precedent — and returns its combined
// output and error. rm ships behind experimental.Guard (state-modifying
// net-new command); the disabled state is covered by
// TestHandleRmRunE_ExperimentalDisabled.
func runRm(t *testing.T, args ...string) (string, error) {
	t.Helper()

	cmd := env.NewRmCmd()
	cmd.Flags().Bool(flags.ExperimentalFlagName, true, "")

	var out bytes.Buffer

	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)

	err := cmd.Execute()

	return out.String(), err
}

//nolint:paralleltest // uses t.Chdir to set the working directory
func TestHandleRmRunE_ExperimentalDisabled(t *testing.T) {
	repoRoot := writeAddEnvSourceRepo(t)
	t.Chdir(repoRoot)

	cmd := env.NewRmCmd()

	var out bytes.Buffer

	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"prod"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "experimental")

	// The gate refused before anything was deleted.
	_, statErr := os.Stat(filepath.Join(repoRoot, "ksail.prod.yaml"))
	require.NoError(t, statErr)
}

//nolint:paralleltest // uses t.Chdir to set the working directory
func TestHandleRmRunE_RemovesConfigRetainsOverlay(t *testing.T) {
	repoRoot := writeAddEnvSourceRepo(t)
	t.Chdir(repoRoot)

	out, err := runRm(t, "prod")
	require.NoError(t, err)

	_, statErr := os.Stat(filepath.Join(repoRoot, "ksail.prod.yaml"))
	require.ErrorIs(t, statErr, os.ErrNotExist)

	// The overlay holds user-authored manifests: retained without --purge.
	_, statErr = os.Stat(filepath.Join(repoRoot, "k8s", "clusters", "prod"))
	require.NoError(t, statErr)

	assert.Contains(t, out, "retained")
	assert.Contains(t, out, "--purge")
}

//nolint:paralleltest // uses t.Chdir to set the working directory
func TestHandleRmRunE_PurgeRemovesOverlay(t *testing.T) {
	repoRoot := writeAddEnvSourceRepo(t)
	t.Chdir(repoRoot)

	out, err := runRm(t, "prod", "--purge")
	require.NoError(t, err)

	_, statErr := os.Stat(filepath.Join(repoRoot, "ksail.prod.yaml"))
	require.ErrorIs(t, statErr, os.ErrNotExist)

	_, statErr = os.Stat(filepath.Join(repoRoot, "k8s", "clusters", "prod"))
	require.ErrorIs(t, statErr, os.ErrNotExist)

	assert.Contains(t, out, "removed environment")
}

//nolint:paralleltest // uses t.Chdir to set the working directory
func TestHandleRmRunE_PurgeWithoutOverlay(t *testing.T) {
	repoRoot := writeAddEnvSourceRepo(t)
	require.NoError(t, os.RemoveAll(filepath.Join(repoRoot, "k8s", "clusters", "prod")))
	t.Chdir(repoRoot)

	out, err := runRm(t, "prod", "--purge")
	require.NoError(t, err)

	assert.Contains(t, out, "nothing to purge")
}

//nolint:paralleltest // uses t.Chdir to set the working directory
func TestHandleRmRunE_MissingEnvironmentListsAvailable(t *testing.T) {
	repoRoot := writeAddEnvSourceRepo(t)
	t.Chdir(repoRoot)

	_, err := runRm(t, "nosuch")
	// The default removal no longer loads the config (only --purge does), so a
	// mistyped name surfaces as the missing config file — still enriched with
	// what is actually declared.
	require.ErrorIs(t, err, environment.ErrEnvironmentConfigMissing)
	assert.Contains(t, err.Error(), "available environments: prod")

	// Nothing was deleted on the failed lookup.
	_, statErr := os.Stat(filepath.Join(repoRoot, "ksail.prod.yaml"))
	require.NoError(t, statErr)
}

//nolint:paralleltest // uses t.Chdir to set the working directory
func TestHandleRmRunE_InvalidName(t *testing.T) {
	repoRoot := writeAddEnvSourceRepo(t)
	t.Chdir(repoRoot)

	_, err := runRm(t, "bad/name")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid environment name")
}

// TestHandleRmRunE_EmptyName pins the empty-name guard: ValidateClusterName
// accepts "" (empty means "use the default cluster"), but an empty
// environment name would target the malformed ksail..yaml — and rm would
// DELETE it if present — so the env verbs reject "" explicitly before any
// path is constructed (ksail#6059 review).
//
//nolint:paralleltest // uses t.Chdir to set the working directory
func TestHandleRmRunE_EmptyName(t *testing.T) {
	repoRoot := writeAddEnvSourceRepo(t)

	// A stray malformed ksail..yaml must survive an empty-name rm.
	strayConfig := filepath.Join(repoRoot, "ksail..yaml")
	require.NoError(t, os.WriteFile(strayConfig, []byte("# stray\n"), 0o600))
	t.Chdir(repoRoot)

	_, err := runRm(t, "")
	require.ErrorIs(t, err, env.ErrEmptyEnvironmentName)

	_, statErr := os.Stat(strayConfig)
	require.NoError(t, statErr, "an empty-name rm must not touch ksail..yaml")
}

// TestNewRmCmd_Structure pins the command shape of `project env rm`: its Use
// line, the `remove` alias, the --purge flag, and the write permission
// annotation that marks it state-modifying.
func TestNewRmCmd_Structure(t *testing.T) {
	t.Parallel()

	cmd := env.NewRmCmd()

	assert.Equal(t, "rm <name>", cmd.Use)
	assert.Equal(t, []string{"remove"}, cmd.Aliases)
	assert.NotEmpty(t, cmd.Short)

	require.NotNil(t, cmd.Flags().Lookup("purge"))

	// The write annotation marks the command as state-modifying.
	assert.Equal(t, "write", cmd.Annotations[annotations.AnnotationPermission])
}

//nolint:paralleltest // uses t.Chdir to set the working directory
func TestHandleRmRunE_NonPurgeSucceedsOnUnloadableConfig(t *testing.T) {
	repoRoot := writeAddEnvSourceRepo(t)
	require.NoError(t, os.WriteFile(
		filepath.Join(repoRoot, "ksail.prod.yaml"),
		[]byte(":\tthis is not yaml"),
		0o600,
	))
	t.Chdir(repoRoot)

	// The default removal only deletes the root config; it must not require
	// that config to load (only --purge needs its sourceDirectory).
	out, err := runRm(t, "prod")
	require.NoError(t, err)
	assert.Contains(t, out, "removed environment")

	_, statErr := os.Stat(filepath.Join(repoRoot, "ksail.prod.yaml"))
	require.ErrorIs(t, statErr, os.ErrNotExist)

	// The overlay is untouched (and the retained-overlay hint is skipped,
	// since locating the overlay needs the unloadable config).
	_, statErr = os.Stat(filepath.Join(repoRoot, "k8s", "clusters", "prod"))
	require.NoError(t, statErr)
}

//nolint:paralleltest // uses t.Chdir to set the working directory
func TestHandleRmRunE_PurgeFailureRetainsConfig(t *testing.T) {
	repoRoot := writeAddEnvSourceRepo(t)

	// Turn k8s/clusters into a symlink to an outside directory holding the
	// overlay: the purge must refuse to traverse it — and, refusing, must leave
	// the environment config in place so a retry still sees the environment.
	outside := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(outside, "prod"), 0o750))
	clustersDir := filepath.Join(repoRoot, "k8s", "clusters")
	require.NoError(t, os.RemoveAll(clustersDir))
	require.NoError(t, os.Symlink(outside, clustersDir))
	t.Chdir(repoRoot)

	_, err := runRm(t, "prod", "--purge")
	require.Error(t, err)

	// Config retained: the failed purge did not un-declare the environment.
	_, statErr := os.Stat(filepath.Join(repoRoot, "ksail.prod.yaml"))
	require.NoError(t, statErr)

	// The outside target was not deleted.
	_, statErr = os.Stat(filepath.Join(outside, "prod"))
	require.NoError(t, statErr)
}

//nolint:paralleltest // uses t.Chdir to set the working directory
func TestHandleRmRunE_PurgeAppliesDefaultSourceDirectory(t *testing.T) {
	// A config relying on the documented sourceDirectory default ("k8s") must
	// purge k8s/clusters/<name>, not clusters/<name>: the silent loader does
	// not apply field defaults, so the purge path has to.
	repoRoot := writeAddEnvSourceRepo(t)

	defaultRelying := strings.Replace(addEnvSourceConfig, "    sourceDirectory: k8s\n", "", 1)
	require.NotEqual(t, addEnvSourceConfig, defaultRelying,
		"fixture must drop the explicit sourceDirectory")
	require.NoError(t,
		os.WriteFile(filepath.Join(repoRoot, "ksail.prod.yaml"), []byte(defaultRelying), 0o600))
	t.Chdir(repoRoot)

	_, err := runRm(t, "prod", "--purge")
	require.NoError(t, err)

	_, statErr := os.Stat(filepath.Join(repoRoot, "k8s", "clusters", "prod"))
	require.ErrorIs(t, statErr, os.ErrNotExist, "the real overlay must be purged")
}
