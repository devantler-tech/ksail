package env_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/project/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runRm executes the rm command standalone with args and returns its combined
// output and error.
func runRm(t *testing.T, args ...string) (string, error) {
	t.Helper()

	cmd := env.NewRmCmd()

	var out bytes.Buffer

	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)

	err := cmd.Execute()

	return out.String(), err
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
	require.ErrorIs(t, err, env.ErrSourceConfigLoad)
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
