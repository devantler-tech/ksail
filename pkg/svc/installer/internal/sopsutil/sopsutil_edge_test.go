package sopsutil_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/svc/installer/internal/sopsutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestResolveAgeKey_KeyFileReadError tests that a key file path that is
// a directory (not a regular file) returns an error from os.ReadFile.
//

func TestResolveAgeKey_KeyFileReadError(t *testing.T) {
	// Create a directory where key file should be
	dir := t.TempDir()
	keyDir := filepath.Join(dir, "keys.txt")
	err := os.Mkdir(keyDir, 0o700)
	require.NoError(t, err)

	t.Setenv("SOPS_AGE_KEY_FILE", keyDir)
	t.Setenv("TEST_SOPSUTIL_NONEXISTENT_READ_ERR", "")

	sops := v1alpha1.SOPS{AgeKeyEnvVar: "TEST_SOPSUTIL_NONEXISTENT_READ_ERR"}

	got, resolveErr := sopsutil.ResolveAgeKey(sops)
	require.Error(t, resolveErr)
	assert.Empty(t, got)
	assert.Contains(t, resolveErr.Error(), "read age key file")
}

// TestResolveEnabledAgeKey_ExplicitlyEnabledWithResolveError tests the path where
// SOPS is explicitly enabled and ResolveAgeKey returns an error.
//

func TestResolveEnabledAgeKey_ExplicitlyEnabledWithResolveError(t *testing.T) {
	// Create a key file path that is a directory to trigger a read error
	dir := t.TempDir()
	keyDir := filepath.Join(dir, "keys.txt")
	err := os.Mkdir(keyDir, 0o700)
	require.NoError(t, err)

	t.Setenv("SOPS_AGE_KEY_FILE", keyDir)
	t.Setenv("TEST_SOPSUTIL_NONEXISTENT_ENABLED_ERR", "")

	enabled := true
	sops := v1alpha1.SOPS{
		Enabled:      &enabled,
		AgeKeyEnvVar: "TEST_SOPSUTIL_NONEXISTENT_ENABLED_ERR",
	}

	got, resolveErr := sopsutil.ResolveEnabledAgeKey(sops)
	require.Error(t, resolveErr)
	assert.Empty(t, got)
}

// TestResolveAgeKey_EnvVarWithInvalidKey tests that an env var containing
// text without the AGE-SECRET-KEY prefix returns empty.
//

func TestResolveAgeKey_EnvVarWithInvalidKey(t *testing.T) {
	t.Setenv("TEST_SOPSUTIL_INVALID_KEY", "not-a-valid-age-key")
	t.Setenv("SOPS_AGE_KEY_FILE", filepath.Join(t.TempDir(), "nonexistent-keys.txt"))

	sops := v1alpha1.SOPS{AgeKeyEnvVar: "TEST_SOPSUTIL_INVALID_KEY"}

	got, err := sopsutil.ResolveAgeKey(sops)
	require.NoError(t, err)
	assert.Empty(t, got)
}
