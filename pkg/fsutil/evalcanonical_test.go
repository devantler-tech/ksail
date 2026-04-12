package fsutil_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v6/pkg/fsutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEvalCanonicalPath(t *testing.T) {
	t.Parallel()

	t.Run("resolves existing path", testEvalCanonicalPathExisting)
	t.Run("resolves non-existing path via parent", testEvalCanonicalPathNonExisting)
	t.Run("resolves symlink to real path", testEvalCanonicalPathSymlink)
	t.Run("returns error for invalid parent", testEvalCanonicalPathInvalidParent)
}

func testEvalCanonicalPathExisting(t *testing.T) {
	t.Helper()
	t.Parallel()

	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "test.txt")
	err := os.WriteFile(filePath, []byte("test"), 0o600)
	require.NoError(t, err, "WriteFile")

	result, err := fsutil.EvalCanonicalPath(filePath)

	require.NoError(t, err, "EvalCanonicalPath")
	assert.True(t, filepath.IsAbs(result), "should return absolute path")

	// Resolve tempDir itself to handle macOS /private/var vs /var symlinks
	resolvedTempDir, resolveErr := filepath.EvalSymlinks(tempDir)
	require.NoError(t, resolveErr)
	expected := filepath.Join(resolvedTempDir, "test.txt")
	assert.Equal(t, expected, result, "should match resolved path")
}

func testEvalCanonicalPathNonExisting(t *testing.T) {
	t.Helper()
	t.Parallel()

	tempDir := t.TempDir()
	nonExisting := filepath.Join(tempDir, "future-file.txt")

	result, err := fsutil.EvalCanonicalPath(nonExisting)

	require.NoError(t, err, "EvalCanonicalPath for non-existing file")
	assert.True(t, filepath.IsAbs(result), "should return absolute path")
	assert.Contains(t, result, "future-file.txt", "should contain filename")
}

func testEvalCanonicalPathSymlink(t *testing.T) {
	t.Helper()
	t.Parallel()

	tempDir := t.TempDir()
	realFile := filepath.Join(tempDir, "real.txt")
	err := os.WriteFile(realFile, []byte("real"), 0o600)
	require.NoError(t, err, "WriteFile")

	linkPath := filepath.Join(tempDir, "link.txt")
	err = os.Symlink(realFile, linkPath)
	require.NoError(t, err, "Symlink")

	result, err := fsutil.EvalCanonicalPath(linkPath)

	require.NoError(t, err, "EvalCanonicalPath for symlink")

	// Resolve tempDir to handle macOS symlinks
	resolvedTempDir, resolveErr := filepath.EvalSymlinks(tempDir)
	require.NoError(t, resolveErr)
	expected := filepath.Join(resolvedTempDir, "real.txt")
	assert.Equal(t, expected, result, "should resolve through symlink")
}

func testEvalCanonicalPathInvalidParent(t *testing.T) {
	t.Helper()
	t.Parallel()

	// Use a path whose parent directory also does not exist
	invalidPath := filepath.Join("/nonexistent-dir-abc123", "subdir", "file.txt")

	_, err := fsutil.EvalCanonicalPath(invalidPath)

	require.Error(t, err, "should fail for invalid parent")
}

// TestEvalCanonicalPath_PermissionDenied verifies behavior when symlink resolution
// fails with a non-NotExist error (e.g., permission denied).
func TestEvalCanonicalPath_PermissionDenied(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	// Create a directory with no read/execute permission
	restrictedDir := filepath.Join(tempDir, "restricted")
	err := os.Mkdir(restrictedDir, 0o000)
	require.NoError(t, err)

	// Try to resolve a path inside the restricted directory
	// This should fail with a permission error (not NotExist)
	targetPath := filepath.Join(restrictedDir, "subdir", "file.txt")

	_, err = fsutil.EvalCanonicalPath(targetPath)
	// On some OS configurations root might bypass permissions, so we just
	// verify we either get an error or a valid result
	if err != nil {
		assert.Contains(t, err.Error(), "resolving symlinks")
	}
}

// TestEvalCanonicalPath_DotDotInPath verifies resolution of paths containing .. components.
func TestEvalCanonicalPath_DotDotInPath(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	subDir := filepath.Join(tempDir, "sub")
	err := os.Mkdir(subDir, 0o700)
	require.NoError(t, err)

	filePath := filepath.Join(tempDir, "file.txt")
	err = os.WriteFile(filePath, []byte("test"), 0o600)
	require.NoError(t, err)

	// Resolve path with .. component
	pathWithDots := filepath.Join(subDir, "..", "file.txt")
	result, err := fsutil.EvalCanonicalPath(pathWithDots)

	require.NoError(t, err)

	resolvedTempDir, _ := filepath.EvalSymlinks(tempDir)
	expected := filepath.Join(resolvedTempDir, "file.txt")
	assert.Equal(t, expected, result)
}
