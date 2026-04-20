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

// TestResolveAgeKey_KeyFilePermissionDenied tests the os.ReadFile error path
// when the key file exists but is not readable (permission denied).
func TestResolveAgeKey_KeyFilePermissionDenied(t *testing.T) {
	skipPermissionSensitiveTest(t)

	dir := t.TempDir()
	keyPath := filepath.Join(dir, "keys.txt")
	// Create the file, then remove read permission
	err := os.WriteFile(keyPath, []byte("AGE-SECRET-KEY-1ABC"), 0o000)
	require.NoError(t, err)

	t.Setenv("SOPS_AGE_KEY_FILE", keyPath)

	sops := v1alpha1.SOPS{AgeKeyEnvVar: "TEST_SOPSUTIL_NONEXISTENT_PERM"}

	_, resolveErr := sopsutil.ResolveAgeKey(sops)
	require.Error(t, resolveErr)
	assert.Contains(t, resolveErr.Error(), "read age key file")
}
