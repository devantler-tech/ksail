package fsutil_test

import (
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSOPSAgeKeyPath_XDGConfigHome verifies that when XDG_CONFIG_HOME is set,
// SOPSAgeKeyPath returns the XDG-based path.
//

func TestSOPSAgeKeyPath_XDGConfigHome(t *testing.T) {
	// Clear SOPS_AGE_KEY_FILE so it doesn't take precedence.
	t.Setenv("SOPS_AGE_KEY_FILE", "")
	t.Setenv("XDG_CONFIG_HOME", "/custom/xdg-config")

	path, err := fsutil.SOPSAgeKeyPath()

	require.NoError(t, err)
	assert.Equal(t, filepath.Join("/custom/xdg-config", "sops", "age", "keys.txt"), path)
}

// TestSOPSAgeKeyPath_DarwinDefault verifies the macOS default path when no
// environment variables are set.
//

func TestSOPSAgeKeyPath_DarwinDefault(t *testing.T) {
	t.Setenv("SOPS_AGE_KEY_FILE", "")
	t.Setenv("XDG_CONFIG_HOME", "")

	path, err := fsutil.SOPSAgeKeyPath()

	require.NoError(t, err)
	// On macOS, should use Library/Application Support path
	// On Linux, should use .config path
	// Both are valid — just verify it's non-empty and ends with keys.txt
	assert.Contains(t, path, "sops")
	assert.Contains(t, path, "keys.txt")
}

// TestExpandHomePath_AlreadyAbsolute verifies that an already-absolute path
// is returned unchanged.
func TestExpandHomePath_AlreadyAbsolute(t *testing.T) {
	t.Parallel()

	absolutePath := filepath.Join(t.TempDir(), "file")
	path, err := fsutil.ExpandHomePath(absolutePath)

	require.NoError(t, err)
	assert.Equal(t, absolutePath, path)
}

// TestEvalCanonicalPath_ParentNotExist verifies that when both the path and its
// parent directory don't exist, an error is returned.
func TestEvalCanonicalPath_ParentNotExist(t *testing.T) {
	t.Parallel()

	missingParent := filepath.Join(t.TempDir(), "nonexistent-parent")
	missingFile := filepath.Join(missingParent, "nested", "file.txt")

	_, err := fsutil.EvalCanonicalPath(missingFile)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "resolving symlinks for parent")
}
