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

func TestResolveAgeKey_FromKeyFile(t *testing.T) {
	const testKey = "AGE-SECRET-KEY-1FILETEST0000000000000000000000000000000000000000000000"

	dir := t.TempDir()
	keyPath := filepath.Join(dir, "keys.txt")
	err := os.WriteFile(keyPath, []byte("# comment\n"+testKey+"\n"), 0o600)
	require.NoError(t, err)

	t.Setenv("SOPS_AGE_KEY_FILE", keyPath)
	t.Setenv("TEST_SOPSUTIL_NONEXISTENT_12345", "")

	sops := v1alpha1.SOPS{AgeKeyEnvVar: "TEST_SOPSUTIL_NONEXISTENT_12345"}

	got, resolveErr := sopsutil.ResolveAgeKey(sops)
	require.NoError(t, resolveErr)
	assert.Equal(t, testKey, got)
}

func TestResolveAgeKey_KeyFileNoKey(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "keys.txt")
	err := os.WriteFile(keyPath, []byte("no key here\n"), 0o600)
	require.NoError(t, err)

	t.Setenv("SOPS_AGE_KEY_FILE", keyPath)
	t.Setenv("TEST_SOPSUTIL_NONEXISTENT_67890", "")

	sops := v1alpha1.SOPS{AgeKeyEnvVar: "TEST_SOPSUTIL_NONEXISTENT_67890"}

	got, resolveErr := sopsutil.ResolveAgeKey(sops)
	require.NoError(t, resolveErr)
	assert.Empty(t, got)
}

func TestResolveEnabledAgeKey_AutoDetectWithError(t *testing.T) {
	// Point SOPS_AGE_KEY_FILE to a directory (not a file) to trigger a read error.
	dir := t.TempDir()
	t.Setenv("SOPS_AGE_KEY_FILE", dir)
	t.Setenv("TEST_SOPSUTIL_NONEXISTENT_99999", "")

	// Auto-detect mode (Enabled == nil), env var unset.
	sops := v1alpha1.SOPS{AgeKeyEnvVar: "TEST_SOPSUTIL_NONEXISTENT_99999"}

	// Auto-detect suppresses errors and returns empty.
	got, err := sopsutil.ResolveEnabledAgeKey(sops)
	require.NoError(t, err)
	assert.Empty(t, got)
}
