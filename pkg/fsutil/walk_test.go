package fsutil_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	"github.com/stretchr/testify/require"
)

// errBoom is a sentinel error used to assert predicate errors abort the walk.
var errBoom = errors.New("boom")

// keepYAML matches .yaml/.yml files.
func keepYAML(path string, _ fs.DirEntry) (bool, error) {
	ext := strings.ToLower(filepath.Ext(path))

	return ext == ".yaml" || ext == ".yml", nil
}

func TestWalkFiles_ReturnsMatchingFilesAndSkipsDirs(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "sub"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(root, "a.yaml"), []byte("a"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "sub", "b.yml"), []byte("b"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "c.txt"), []byte("c"), 0o600))

	files, err := fsutil.WalkFiles(root, keepYAML)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{
		filepath.Join(root, "a.yaml"),
		filepath.Join(root, "sub", "b.yml"),
	}, files)
}

func TestWalkFiles_SingleFileRoot(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	file := filepath.Join(root, "only.yaml")
	require.NoError(t, os.WriteFile(file, []byte("x"), 0o600))

	files, err := fsutil.WalkFiles(file, keepYAML)
	require.NoError(t, err)
	require.Equal(t, []string{file}, files)
}

func TestWalkFiles_PredicateErrorAborts(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "a.yaml"), []byte("a"), 0o600))

	_, err := fsutil.WalkFiles(root, func(_ string, _ fs.DirEntry) (bool, error) {
		return false, errBoom
	})
	require.ErrorIs(t, err, errBoom)
}

func TestWalkFiles_NonexistentRootErrors(t *testing.T) {
	t.Parallel()

	_, err := fsutil.WalkFiles(filepath.Join(t.TempDir(), "nope"), keepYAML)
	require.Error(t, err)
}
