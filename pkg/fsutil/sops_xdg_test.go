package fsutil_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v6/pkg/fsutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSOPSAgeKeyPath_XDGConfigHome verifies that when XDG_CONFIG_HOME is set,
// SOPSAgeKeyPath returns the XDG-based path.
//
//nolint:paralleltest // Uses t.Setenv
func TestSOPSAgeKeyPath_XDGConfigHome(t *testing.T) {
	// Clear SOPS_AGE_KEY_FILE so it doesn't take precedence.
	t.Setenv("SOPS_AGE_KEY_FILE", "")
	t.Setenv("XDG_CONFIG_HOME", "/custom/xdg-config")

	path, err := fsutil.SOPSAgeKeyPath()

	require.NoError(t, err)
	assert.Equal(t, "/custom/xdg-config/sops/age/keys.txt", path)
}

// TestSOPSAgeKeyPath_DarwinDefault verifies the macOS default path when no
// environment variables are set.
//
//nolint:paralleltest // Uses t.Setenv
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

	path, err := fsutil.ExpandHomePath("/absolute/path/to/file")

	require.NoError(t, err)
	assert.Equal(t, "/absolute/path/to/file", path)
}

// TestEvalCanonicalPath_ParentNotExist verifies that when both the path and its
// parent directory don't exist, an error is returned.
func TestEvalCanonicalPath_ParentNotExist(t *testing.T) {
	t.Parallel()

	_, err := fsutil.EvalCanonicalPath("/nonexistent-dir-for-fsutil-test/nested/file.txt")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "resolving symlinks for parent")
}
