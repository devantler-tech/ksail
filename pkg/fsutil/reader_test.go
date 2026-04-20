package fsutil_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Tests are intentionally minimal and explicit to keep coverage high and behavior clear.
func TestReadFileSafe(t *testing.T) {
	t.Parallel()

	t.Run("normal read", testReadFileSafeNormalRead)
	t.Run("outside base", testReadFileSafeOutsideBase)
	t.Run("traversal attempt", testReadFileSafeTraversalAttempt)
	t.Run("prefix attack - sibling directory", testReadFileSafePrefixAttack)
	t.Run("path with ..evil dir inside base", testReadFileSafeEvilDirInsideBase)
	t.Run("symlink escape", testReadFileSafeSymlinkEscape)
	t.Run("missing file inside base", testReadFileSafeMissingFile)
}

func testReadFileSafeNormalRead(t *testing.T) {
	t.Helper()
	t.Parallel()

	base := t.TempDir()
	filePath := filepath.Join(base, "file.txt")
	want := "hello safe"
	err := os.WriteFile(filePath, []byte(want), 0o600)
	require.NoError(t, err, "WriteFile setup")

	got, err := fsutil.ReadFileSafe(base, filePath)

	require.NoError(t, err, "ReadFileSafe")
	assert.Equal(t, want, string(got), "content")
}

func testReadFileSafeOutsideBase(t *testing.T) {
	t.Helper()
	t.Parallel()

	base := t.TempDir()
	outsideDir := t.TempDir()
	outside := filepath.Join(outsideDir, "outside-test-file.txt")
	err := os.WriteFile(outside, []byte("nope"), 0o600)
	require.NoError(t, err, "WriteFile setup")

	_, err = fsutil.ReadFileSafe(base, outside)
	require.ErrorIs(t, err, fsutil.ErrPathOutsideBase, "ReadFileSafe")
}

func testReadFileSafeTraversalAttempt(t *testing.T) {
	t.Helper()
	t.Parallel()

	base := t.TempDir()
	parent := filepath.Join(base, "..", "traversal.txt")
	absParent, err := filepath.Abs(parent)
	require.NoError(t, err, "Abs parent")
	err = os.WriteFile(absParent, []byte("traversal"), 0o600)
	require.NoError(t, err, "WriteFile setup parent")

	attempt := filepath.Join(base, "..", "traversal.txt")

	_, err = fsutil.ReadFileSafe(base, attempt)
	require.ErrorIs(t, err, fsutil.ErrPathOutsideBase, "ReadFileSafe")
}

func testReadFileSafePrefixAttack(t *testing.T) {
	t.Helper()
	t.Parallel()

	// Verify that a sibling directory whose path has basePath as a string-prefix
	// is correctly rejected. e.g. base="/tmp/dir", evil="/tmp/dir-evil".
	parent := t.TempDir()
	base := filepath.Join(parent, "dir")
	sibling := filepath.Join(parent, "dir-evil")

	err := os.Mkdir(base, 0o700)
	require.NoError(t, err, "Mkdir base")
	err = os.Mkdir(sibling, 0o700)
	require.NoError(t, err, "Mkdir sibling")

	secretFile := filepath.Join(sibling, "secret.txt")
	err = os.WriteFile(secretFile, []byte("secret"), 0o600)
	require.NoError(t, err, "WriteFile secret")

	_, err = fsutil.ReadFileSafe(base, secretFile)
	require.ErrorIs(t, err, fsutil.ErrPathOutsideBase, "ReadFileSafe prefix attack")
}

func testReadFileSafeEvilDirInsideBase(t *testing.T) {
	t.Helper()
	t.Parallel()

	// Verify that a directory named "..evil" inside basePath is accepted.
	// The relative path "..evil/file.txt" starts with ".." as a string but
	// is NOT a parent-directory traversal; only rel == ".." or "../..." should be rejected.
	base := t.TempDir()
	evilDir := filepath.Join(base, "..evil")
	err := os.Mkdir(evilDir, 0o700)
	require.NoError(t, err, "Mkdir ..evil")

	secretFile := filepath.Join(evilDir, "file.txt")
	want := "safe inside base"
	err = os.WriteFile(secretFile, []byte(want), 0o600)
	require.NoError(t, err, "WriteFile inside ..evil")

	got, err := fsutil.ReadFileSafe(base, secretFile)

	require.NoError(t, err, "ReadFileSafe ..evil inside base")
	assert.Equal(t, want, string(got), "content")
}

func testReadFileSafeSymlinkEscape(t *testing.T) {
	t.Helper()
	t.Parallel()

	// Verify that a symlink inside basePath that resolves to a file outside
	// basePath is rejected. Without symlink canonicalization the path "/base/link"
	// passes the rel-based check, but os.ReadFile would follow the symlink and
	// read outside the intended base directory.
	outsideDir := t.TempDir()
	secretFile := filepath.Join(outsideDir, "secret.txt")
	err := os.WriteFile(secretFile, []byte("secret"), 0o600)
	require.NoError(t, err, "WriteFile secret")

	base := t.TempDir()
	linkPath := filepath.Join(base, "link.txt")
	err = os.Symlink(secretFile, linkPath)
	require.NoError(t, err, "Symlink")

	_, err = fsutil.ReadFileSafe(base, linkPath)
	require.ErrorIs(t, err, fsutil.ErrPathOutsideBase, "ReadFileSafe symlink escape")
}

func testReadFileSafeMissingFile(t *testing.T) {
	t.Helper()
	t.Parallel()

	base := t.TempDir()
	missing := filepath.Join(base, "missing.txt")

	_, err := fsutil.ReadFileSafe(base, missing)
	assert.ErrorContains(t, err, "failed to read file", "ReadFileSafe")
}

//nolint:paralleltest,tparallel // Cannot use t.Parallel() with t.Chdir()
func TestFindFile(t *testing.T) {
	t.Run("absolute path", testFindFileAbsolutePath)
	//nolint:paralleltest // Cannot use t.Parallel() with t.Chdir()
	t.Run("relative path found in current directory", testFindFileRelativePathCurrent)
	//nolint:paralleltest // Cannot use t.Parallel() with t.Chdir()
	t.Run("relative path found in parent directory", testFindFileRelativePathParent)
	//nolint:paralleltest // Cannot use t.Parallel() with t.Chdir()
	t.Run("relative path not found", testFindFileRelativePathNotFound)
	//nolint:paralleltest // Cannot use t.Parallel() with t.Chdir()
	t.Run("relative path traversal multiple levels", testFindFileRelativePathMultipleLevels)
}

func testFindFileAbsolutePath(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	absolutePath := filepath.Join(tempDir, "config.yaml")
	err := os.WriteFile(absolutePath, []byte("test"), 0o600)
	require.NoError(t, err)

	resolved, err := fsutil.FindFile(absolutePath)

	require.NoError(t, err)
	assert.Equal(t, absolutePath, resolved)
}

func testFindFileRelativePathCurrent(t *testing.T) {
	// Create a temporary directory and change to it
	tempDir := t.TempDir()

	t.Chdir(tempDir)

	// Create config file in current directory
	configFile := "test-config.yaml"
	err := os.WriteFile(configFile, []byte("test"), 0o600)
	require.NoError(t, err)

	resolved, err := fsutil.FindFile(configFile)

	require.NoError(t, err)

	expectedPath := filepath.Join(tempDir, configFile)
	assert.Equal(t, expectedPath, resolved)
}

func testFindFileRelativePathParent(t *testing.T) {
	// Create a temporary directory structure
	tempDir := t.TempDir()

	// Create config file in temp directory
	configFile := "parent-config.yaml"
	configPath := filepath.Join(tempDir, configFile)
	err := os.WriteFile(configPath, []byte("test"), 0o600)
	require.NoError(t, err)

	// Create subdirectory and change to it
	subDir := filepath.Join(tempDir, "subdir")
	err = os.Mkdir(subDir, 0o750)
	require.NoError(t, err)

	t.Chdir(subDir)

	resolved, err := fsutil.FindFile(configFile)

	require.NoError(t, err)
	assert.Equal(t, configPath, resolved)
}

func testFindFileRelativePathNotFound(t *testing.T) {
	tempDir := t.TempDir()

	t.Chdir(tempDir)

	configFile := "non-existent-config.yaml"

	resolved, err := fsutil.FindFile(configFile)

	require.NoError(t, err)
	// Should return original path when not found
	assert.Equal(t, configFile, resolved)
}

func testFindFileRelativePathMultipleLevels(t *testing.T) {
	// Create a deep directory structure
	tempDir := t.TempDir()

	// Create config file at root level
	configFile := "deep-config.yaml"
	configPath := filepath.Join(tempDir, configFile)
	err := os.WriteFile(configPath, []byte("test"), 0o600)
	require.NoError(t, err)

	// Create nested subdirectories
	deepDir := filepath.Join(tempDir, "level1", "level2", "level3")
	err = os.MkdirAll(deepDir, 0o750)
	require.NoError(t, err)

	t.Chdir(deepDir)

	resolved, err := fsutil.FindFile(configFile)

	require.NoError(t, err)
	assert.Equal(t, configPath, resolved)
}
