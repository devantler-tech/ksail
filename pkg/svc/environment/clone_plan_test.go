package environment_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/environment"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPreparedConfigCloneWritesOnlyItsValidatedSnapshot(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	const source = "metadata: {name: local}\nspec: {}\nnote: captured\n"
	require.NoError(t, os.WriteFile(filepath.Join(root, "ksail.local.yaml"), []byte(source), 0o600))
	clone, err := environment.PrepareEnvironmentConfigCloneForTarget(root,
		"ksail.local.yaml", "ksail.prod.yaml", nil,
		environment.CloneTarget{Identity: environment.Identity{Name: "prod"}})
	require.NoError(t, err)
	assert.NoFileExists(t, filepath.Join(root, "ksail.prod.yaml"), "preparation must not write")

	const changedSource = "this source changed after validation\n"
	require.NoError(
		t,
		os.WriteFile(filepath.Join(root, "ksail.local.yaml"), []byte(changedSource), 0o600),
	)

	path, wrote, err := clone.Write(false)
	require.NoError(t, err)
	assert.True(t, wrote)
	assert.Equal(t, "ksail.prod.yaml", path)
	assert.Contains(t, readClone(t, root, path), "name: prod")
	assert.Contains(t, readClone(t, root, path), "note: captured")
	assert.Equal(t, changedSource, readClone(t, root, "ksail.local.yaml"))
}

func TestPreparedConfigCloneRechecksDestinationOnWrite(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "ksail.local.yaml"),
		[]byte("metadata: {name: local}\nspec: {}\n"), 0o600))
	clone, err := environment.PrepareEnvironmentConfigCloneForTarget(root,
		"ksail.local.yaml", "ksail.prod.yaml", nil,
		environment.CloneTarget{Identity: environment.Identity{Name: "prod"}})
	require.NoError(t, err)
	outside := filepath.Join(t.TempDir(), "retained.yaml")
	require.NoError(t, os.WriteFile(outside, []byte("retained\n"), 0o600))
	require.NoError(t, os.Symlink(outside, filepath.Join(root, "ksail.prod.yaml")))

	_, wrote, err := clone.Write(true)
	require.ErrorIs(t, err, environment.ErrDestinationEscapesRepository)
	assert.False(t, wrote)

	assert.Equal(t, "retained\n", readClone(t, filepath.Dir(outside), filepath.Base(outside)))

	var empty environment.ConfigClone

	_, wrote, err = empty.Write(true)
	require.ErrorIs(t, err, environment.ErrInvalidConfig)
	assert.False(t, wrote)
}

//nolint:paralleltest // changing the caller's working directory is the regression
func TestPreparedConfigCloneRetainsItsResolvedRepository(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "workspace")
	require.NoError(t, os.Mkdir(root, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(root, "ksail.local.yaml"),
		[]byte("metadata: {name: local}\nspec: {}\n"), 0o600))
	t.Chdir(parent)

	clone, err := environment.PrepareEnvironmentConfigCloneForTarget("workspace",
		"ksail.local.yaml", "ksail.prod.yaml", nil,
		environment.CloneTarget{Identity: environment.Identity{Name: "prod"}})
	require.NoError(t, err)
	t.Chdir(t.TempDir())

	_, wrote, err := clone.Write(false)
	require.NoError(t, err)
	assert.True(t, wrote)
	assert.Contains(t, readClone(t, root, "ksail.prod.yaml"), "name: prod")
	assert.NoDirExists(t, "workspace")
}
