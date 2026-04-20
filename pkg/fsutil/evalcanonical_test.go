package fsutil_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/devantler-tech/ksail/v7/internal/testutil/rootcheck"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
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

func nonExistentRootPath(t *testing.T, parts ...string) string {
	t.Helper()

	candidateRoot := filepath.Join(t.TempDir(), "ksail-nonexistent-root")

	err := os.RemoveAll(candidateRoot)
	require.NoError(t, err)

	_, err = os.Stat(candidateRoot)
	require.ErrorIs(t, err, os.ErrNotExist)

	pathParts := append([]string{candidateRoot}, parts...)

	return filepath.Join(pathParts...)
}

func skipPermissionSensitivePathTest(t *testing.T) {
	t.Helper()

	if runtime.GOOS == "windows" {
		t.Skip("permission semantics differ on Windows")
	}

	if rootcheck.IsRootUser() {
		t.Skip("running as root — permission checks are bypassed")
	}
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
	skipWindowsSymlinkPrivilegeError(t, err)
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

	invalidPath := nonExistentRootPath(t, "subdir", "file.txt")

	_, err := fsutil.EvalCanonicalPath(invalidPath)

	require.Error(t, err, "should fail for invalid parent")
}

// TestEvalCanonicalPath_PermissionDenied verifies behavior when symlink resolution
// fails with a non-NotExist error (e.g., permission denied).
func TestEvalCanonicalPath_PermissionDenied(t *testing.T) {
	t.Parallel()
	skipPermissionSensitivePathTest(t)

	tempDir := t.TempDir()

	// Create a directory with no read/execute permission
	restrictedDir := filepath.Join(tempDir, "restricted")
	err := os.Mkdir(restrictedDir, 0o000)
	require.NoError(t, err)
	t.Cleanup(func() {
		//nolint:gosec // TempDir cleanup requires execute permission on the directory.
		_ = os.Chmod(restrictedDir, 0o700)
	})

	// Try to resolve a path inside the restricted directory
	// This should fail with a permission error (not NotExist)
	targetPath := filepath.Join(restrictedDir, "subdir", "file.txt")

	_, err = fsutil.EvalCanonicalPath(targetPath)
	require.Error(t, err)
	assert.False(t, os.IsNotExist(err), "expected a non-NotExist error")
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

	resolvedTempDir, err := filepath.EvalSymlinks(tempDir)
	require.NoError(t, err)

	expected := filepath.Join(resolvedTempDir, "file.txt")
	assert.Equal(t, expected, result)
}
