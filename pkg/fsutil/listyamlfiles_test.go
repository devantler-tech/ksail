package fsutil_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListYAMLFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	for _, name := range []string{"a.yaml", "b.yml", "c.txt", "d.json"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600))
	}

	require.NoError(
		t,
		os.Mkdir(filepath.Join(dir, "nested.yaml"), 0o750),
	) // a directory, must be skipped

	files, err := fsutil.ListYAMLFiles(dir)
	require.NoError(t, err)

	bases := make([]string, 0, len(files))
	for _, file := range files {
		bases = append(bases, filepath.Base(file))
		assert.Equal(t, dir, filepath.Dir(file), "paths must be joined with the input dir")
	}

	assert.ElementsMatch(t, []string{"a.yaml", "b.yml"}, bases,
		"only non-directory .yaml/.yml entries should be returned")
}

func TestListYAMLFiles_MissingDirectory(t *testing.T) {
	t.Parallel()

	_, err := fsutil.ListYAMLFiles(filepath.Join(t.TempDir(), "does-not-exist"))
	require.Error(t, err)
}
