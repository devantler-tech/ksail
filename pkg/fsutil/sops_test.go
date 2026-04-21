package fsutil_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//nolint:paralleltest // Cannot use t.Parallel() with t.Setenv()
func TestSOPSAgeKeyPath(t *testing.T) {
	//nolint:paralleltest // Cannot use t.Parallel() with t.Setenv()
	t.Run("returns SOPS_AGE_KEY_FILE when set", testSOPSAgeKeyPathEnvOverride)
	//nolint:paralleltest // Cannot use t.Parallel() with t.Setenv()
	t.Run("returns XDG_CONFIG_HOME path when set", testSOPSAgeKeyPathXDGConfigHome)
	//nolint:paralleltest // Cannot use t.Parallel() with t.Setenv()
	t.Run("returns platform default when no env set", testSOPSAgeKeyPathPlatformDefault)
}

func testSOPSAgeKeyPathEnvOverride(t *testing.T) {
	customPath := "/custom/sops/keys.txt"
	t.Setenv("SOPS_AGE_KEY_FILE", customPath)

	result, err := fsutil.SOPSAgeKeyPath()

	require.NoError(t, err, "SOPSAgeKeyPath")
	assert.Equal(t, customPath, result, "should return SOPS_AGE_KEY_FILE value")
}

func testSOPSAgeKeyPathXDGConfigHome(t *testing.T) {
	xdgDir := "/custom/xdg"

	t.Setenv("SOPS_AGE_KEY_FILE", "")
	t.Setenv("XDG_CONFIG_HOME", xdgDir)

	result, err := fsutil.SOPSAgeKeyPath()

	require.NoError(t, err, "SOPSAgeKeyPath")

	expected := filepath.Join(xdgDir, "sops", "age", "keys.txt")
	assert.Equal(t, expected, result, "should use XDG_CONFIG_HOME")
}

func testSOPSAgeKeyPathPlatformDefault(t *testing.T) {
	t.Setenv("SOPS_AGE_KEY_FILE", "")
	t.Setenv("XDG_CONFIG_HOME", "")

	result, err := fsutil.SOPSAgeKeyPath()
	require.NoError(t, err, "SOPSAgeKeyPath")

	homeDir, homeErr := os.UserHomeDir()
	require.NoError(t, homeErr, "UserHomeDir")

	switch runtime.GOOS {
	case "darwin":
		expected := filepath.Join(
			homeDir,
			"Library",
			"Application Support",
			"sops",
			"age",
			"keys.txt",
		)
		assert.Equal(t, expected, result, "macOS default path")
	case "windows":
		// On Windows without AppData set, this would fail.
		// But since we're testing on the current platform, just verify the suffix.
		assert.Contains(t, result, filepath.Join("sops", "age", "keys.txt"))
	default:
		expected := filepath.Join(homeDir, ".config", "sops", "age", "keys.txt")
		assert.Equal(t, expected, result, "Linux default path")
	}
}
