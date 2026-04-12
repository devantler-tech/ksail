package fsutil_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v6/pkg/fsutil"
	"github.com/stretchr/testify/require"
)

func TestReadFileSafe_InvalidBasePath(t *testing.T) {
	t.Parallel()

	// basePath does not exist at all
	nonExistentBase := filepath.Join(t.TempDir(), "nonexistent-base")
	filePath := filepath.Join(nonExistentBase, "file.txt")

	_, err := fsutil.ReadFileSafe(nonExistentBase, filePath)
	require.ErrorIs(
		t,
		err,
		fsutil.ErrPathOutsideBase,
		"should fail with ErrPathOutsideBase for non-existent base",
	)
}

func TestReadFileSafe_DeepNonExistentBasePath(t *testing.T) {
	t.Parallel()

	// basePath has a parent chain that also doesn't exist — triggers the EvalCanonicalPath(basePath) error path
	deepNonExistent := filepath.Join("/nonexistent-root-abc", "deep", "nested", "base")
	filePath := filepath.Join(deepNonExistent, "file.txt")

	_, err := fsutil.ReadFileSafe(deepNonExistent, filePath)
	require.ErrorIs(
		t,
		err,
		fsutil.ErrPathOutsideBase,
		"should fail with ErrPathOutsideBase for deeply non-existent base",
	)
}

func TestReadFileSafe_SameAsBase(t *testing.T) {
	t.Parallel()

	// Read the base directory itself (which is not a file) — should fail with a read error, not traversal
	base := t.TempDir()

	_, err := fsutil.ReadFileSafe(base, base)
	// Reading a directory as a file fails, but should not report path-outside-base
	require.Error(t, err, "reading a directory as a file should fail")
}

func TestReadFileSafe_NestedSymlinkInsideBase(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	subDir := filepath.Join(base, "sub")
	err := os.Mkdir(subDir, 0o700)
	require.NoError(t, err)

	// Create a real file inside base
	realFile := filepath.Join(base, "real.txt")
	err = os.WriteFile(realFile, []byte("linked content"), 0o600)
	require.NoError(t, err)

	// Create a symlink inside base that points to the real file (also inside base)
	linkPath := filepath.Join(subDir, "link.txt")
	err = os.Symlink(realFile, linkPath)
	require.NoError(t, err)

	// This should succeed because both the link and the target are inside base
	data, err := fsutil.ReadFileSafe(base, linkPath)
	require.NoError(t, err, "symlink inside base pointing inside base should succeed")
	require.Equal(t, "linked content", string(data))
}

func TestReadFileSafe_NonExistentFilePath(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	// Non-existent file, but the parent doesn't exist either
	nonExistent := filepath.Join(base, "nonexistent-subdir", "file.txt")

	_, err := fsutil.ReadFileSafe(base, nonExistent)
	// The parent doesn't exist, so EvalCanonicalPath for the file should resolve through
	// parent fallback, and the file read will fail.
	require.Error(t, err, "should fail for non-existent file path")
}

func TestReadFileSafe_SubdirRead(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	subDir := filepath.Join(base, "nested", "deep")
	err := os.MkdirAll(subDir, 0o750)
	require.NoError(t, err)

	filePath := filepath.Join(subDir, "config.yaml")
	want := "nested-content"
	err = os.WriteFile(filePath, []byte(want), 0o600)
	require.NoError(t, err)

	data, err := fsutil.ReadFileSafe(base, filePath)
	require.NoError(t, err)
	require.Equal(t, want, string(data))
}
