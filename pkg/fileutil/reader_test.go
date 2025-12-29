package fileutil_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/fileutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Tests are intentionally minimal and explicit to keep coverage high and behavior clear.
func TestReadFileSafe(t *testing.T) {
	t.Parallel()

	t.Run("normal read", func(t *testing.T) {
		t.Parallel()

		base := t.TempDir()
		filePath := filepath.Join(base, "file.txt")
		want := "hello safe"
		err := os.WriteFile(filePath, []byte(want), 0o600)
		require.NoError(t, err, "WriteFile setup")

		got, err := fileutil.ReadFileSafe(base, filePath)

		require.NoError(t, err, "ReadFileSafe")
		assert.Equal(t, want, string(got), "content")
	})

	t.Run("outside base", func(t *testing.T) {
		t.Parallel()

		base := t.TempDir()
		outside := filepath.Join(os.TempDir(), "outside-test-file.txt")
		err := os.WriteFile(outside, []byte("nope"), 0o600)
		require.NoError(t, err, "WriteFile setup")

		_, err = fileutil.ReadFileSafe(base, outside)
		require.ErrorIs(t, err, fileutil.ErrPathOutsideBase, "ReadFileSafe")
	})

	t.Run("traversal attempt", func(t *testing.T) {
		t.Parallel()

		base := t.TempDir()
		parent := filepath.Join(base, "..", "traversal.txt")
		absParent, _ := filepath.Abs(parent)
		err := os.WriteFile(absParent, []byte("traversal"), 0o600)
		require.NoError(t, err, "WriteFile setup parent")

		attempt := filepath.Join(base, "..", "traversal.txt")

		_, err = fileutil.ReadFileSafe(base, attempt)
		require.ErrorIs(t, err, fileutil.ErrPathOutsideBase, "ReadFileSafe")
	})

	t.Run("missing file inside base", func(t *testing.T) {
		t.Parallel()

		base := t.TempDir()
		missing := filepath.Join(base, "missing.txt")

		_, err := fileutil.ReadFileSafe(base, missing)
		assert.ErrorContains(t, err, "failed to read file", "ReadFileSafe")
	})
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

	resolved, err := fileutil.FindFile(absolutePath)

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

	resolved, err := fileutil.FindFile(configFile)

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

	resolved, err := fileutil.FindFile(configFile)

	require.NoError(t, err)
	assert.Equal(t, configPath, resolved)
}

func testFindFileRelativePathNotFound(t *testing.T) {
	tempDir := t.TempDir()

	t.Chdir(tempDir)

	configFile := "non-existent-config.yaml"

	resolved, err := fileutil.FindFile(configFile)

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

	resolved, err := fileutil.FindFile(configFile)

	require.NoError(t, err)
	assert.Equal(t, configPath, resolved)
}
