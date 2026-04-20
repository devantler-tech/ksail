package fsutil_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTryWriteFile_CreateNestedDirectories verifies that TryWriteFile creates
// deeply nested directory paths that don't exist yet.
func TestTryWriteFile_CreateNestedDirectories(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	output := filepath.Join(tempDir, "a", "b", "c", "file.txt")
	content := "nested content"

	result, err := fsutil.TryWriteFile(content, output, false)

	require.NoError(t, err)
	assert.Equal(t, content, result)

	// Verify file was created
	data, readErr := os.ReadFile(output) //nolint:gosec // test file
	require.NoError(t, readErr)
	assert.Equal(t, content, string(data))
}

// TestTryWriteFile_CleansDotPath verifies that TryWriteFile cleans paths with .. and . components.
func TestTryWriteFile_CleansDotPath(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	// Create a path with unnecessary dots that Clean should resolve
	output := filepath.Join(tempDir, "sub", "..", "sub", ".", "file.txt")
	content := "cleaned path content"

	result, err := fsutil.TryWriteFile(content, output, false)

	require.NoError(t, err)
	assert.Equal(t, content, result)

	// Verify the file is at the cleaned path
	cleanedPath := filepath.Join(tempDir, "sub", "file.txt")
	data, readErr := os.ReadFile(cleanedPath) //nolint:gosec // test file
	require.NoError(t, readErr)
	assert.Equal(t, content, string(data))
}

// TestTryWriteFile_EmptyContent writes an empty string as content.
func TestTryWriteFile_EmptyContent(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	output := filepath.Join(tempDir, "empty.txt")

	result, err := fsutil.TryWriteFile("", output, false)

	require.NoError(t, err)
	assert.Empty(t, result)

	data, readErr := os.ReadFile(output) //nolint:gosec // test file
	require.NoError(t, readErr)
	assert.Empty(t, data)
}
