package fsutil_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadFileSafe_MissingBasePathReturnsNotExist(t *testing.T) {
	t.Parallel()

	// A missing base path whose parent exists should canonicalize through the
	// existing parent; the eventual failure is reading the missing path itself.
	nonExistentBase := filepath.Join(t.TempDir(), "nonexistent-base")

	_, err := fsutil.ReadFileSafe(nonExistentBase, nonExistentBase)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestReadFileSafe_FileOutsideBaseReturnsOutsideBase(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	outsideFile := filepath.Join(filepath.Dir(base), "outside.txt")
	require.NoError(t, os.WriteFile(outsideFile, []byte("outside"), 0o600))

	traversalPath := filepath.Join(base, "..", "outside.txt")

	_, err := fsutil.ReadFileSafe(base, traversalPath)
	require.ErrorIs(
		t,
		err,
		fsutil.ErrPathOutsideBase,
		"explicit traversal outside the base should be rejected",
	)
}

func TestReadFileSafe_SameAsBase(t *testing.T) {
	t.Parallel()

	// Read the base directory itself (which is not a file) — should fail with a read error, not traversal
	base := t.TempDir()

	_, err := fsutil.ReadFileSafe(base, base)
	// Reading a directory as a file fails, but should not report path-outside-base
	require.Error(t, err, "reading a directory as a file should fail")
	assert.NotErrorIs(
		t,
		err,
		fsutil.ErrPathOutsideBase,
		"reading the base directory itself should not be reported as path-outside-base",
	)
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
	skipWindowsSymlinkPrivilegeError(t, err)
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
