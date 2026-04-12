package sopsutil_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/devantler-tech/ksail/v6/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v6/pkg/svc/installer/internal/sopsutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestResolveAgeKey_EvalCanonicalPathParentNotExist verifies that when the SOPS
// age key file path has a non-existent parent directory, EvalCanonicalPath
// returns a wrapped error and ResolveAgeKey propagates it. This exercises the
// canonicalize error path that is otherwise unreachable with valid parent dirs.
//
//nolint:paralleltest // Uses t.Setenv
func TestResolveAgeKey_EvalCanonicalPathParentNotExist(t *testing.T) {
	// Point SOPS_AGE_KEY_FILE to a path where even the parent directory does
	// not exist. EvalCanonicalPath will fail to resolve the parent's symlinks.
	t.Setenv("SOPS_AGE_KEY_FILE", "/nonexistent-parent-dir-for-sopsutil-test/nested/keys.txt")
	t.Setenv("TEST_SOPSUTIL_CANONICAL_NOT_EXIST", "")

	sops := v1alpha1.SOPS{AgeKeyEnvVar: "TEST_SOPSUTIL_CANONICAL_NOT_EXIST"}

	_, err := sopsutil.ResolveAgeKey(sops)

	require.Error(t, err, "should error when key path parent does not exist")
	assert.Contains(t, err.Error(), "canonicalize age key path")
}

// TestResolveAgeKey_EvalCanonicalPathPermissionDenied verifies that when the
// key file path has a parent directory with no permissions, EvalCanonicalPath
// returns a non-IsNotExist error and ResolveAgeKey propagates it.
//
//nolint:paralleltest // Uses t.Setenv
func TestResolveAgeKey_EvalCanonicalPathPermissionDenied(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission semantics differ on Windows")
	}

	if os.Getuid() == 0 {
		t.Skip("running as root — permission checks are bypassed")
	}

	// Create a directory hierarchy where we remove all permissions from a
	// middle directory. EvalCanonicalPath will fail with a permission error
	// (not IsNotExist).
	baseDir := t.TempDir()
	restrictedDir := filepath.Join(baseDir, "restricted")

	err := os.MkdirAll(restrictedDir, 0o700)
	require.NoError(t, err)

	// Create a subdirectory inside restricted, then lock restricted.
	innerDir := filepath.Join(restrictedDir, "inner")
	err = os.MkdirAll(innerDir, 0o700)
	require.NoError(t, err)

	// Now remove permissions from restrictedDir so that resolving anything
	// inside it fails with EACCES.
	err = os.Chmod(restrictedDir, 0o000)
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Chmod(restrictedDir, 0o700) })

	keyFilePath := filepath.Join(innerDir, "keys.txt")
	t.Setenv("SOPS_AGE_KEY_FILE", keyFilePath)
	t.Setenv("TEST_SOPSUTIL_PERM_DENIED", "")

	sops := v1alpha1.SOPS{AgeKeyEnvVar: "TEST_SOPSUTIL_PERM_DENIED"}

	_, resolveErr := sopsutil.ResolveAgeKey(sops)

	require.Error(t, resolveErr, "should error when parent has no permissions")
	assert.Contains(t, resolveErr.Error(), "canonicalize age key path")
}

// TestResolveEnabledAgeKey_AutoDetectWithCanonicalError verifies that when
// SOPS.Enabled is nil (auto-detect mode) and ResolveAgeKey returns an error,
// ResolveEnabledAgeKey swallows the error and returns ("", nil).
//
//nolint:paralleltest // Uses t.Setenv
func TestResolveEnabledAgeKey_AutoDetectWithCanonicalError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission semantics differ on Windows")
	}

	if os.Getuid() == 0 {
		t.Skip("running as root — permission checks are bypassed")
	}

	baseDir := t.TempDir()
	restrictedDir := filepath.Join(baseDir, "no-access")

	err := os.MkdirAll(filepath.Join(restrictedDir, "inner"), 0o700)
	require.NoError(t, err)

	err = os.Chmod(restrictedDir, 0o000)
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Chmod(restrictedDir, 0o700) })

	t.Setenv("SOPS_AGE_KEY_FILE", filepath.Join(restrictedDir, "inner", "keys.txt"))
	t.Setenv("TEST_SOPSUTIL_AUTODETECT_ERR", "")

	sops := v1alpha1.SOPS{
		AgeKeyEnvVar: "TEST_SOPSUTIL_AUTODETECT_ERR",
		Enabled:      nil, // auto-detect
	}

	key, resolveErr := sopsutil.ResolveEnabledAgeKey(sops)

	require.NoError(t, resolveErr, "auto-detect should swallow ResolveAgeKey errors")
	assert.Empty(t, key)
}
