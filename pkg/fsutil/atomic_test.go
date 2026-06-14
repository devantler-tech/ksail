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

// atomicTempGlob matches the temporary files AtomicWriteFile creates while
// staging a write, so tests can assert they are always cleaned up.
const atomicTempGlob = ".atomic-*.tmp"

// assertNoTempLeftover fails if any staging temp file remains in dir.
func assertNoTempLeftover(t *testing.T, dir string) {
	t.Helper()

	leftovers, globErr := filepath.Glob(filepath.Join(dir, atomicTempGlob))
	require.NoError(t, globErr)
	assert.Empty(t, leftovers, "AtomicWriteFile must not leave staging temp files behind")
}

// TestAtomicWriteFile_WritesContent verifies the happy path writes the exact
// bytes to the target path and removes its staging temp file afterwards.
func TestAtomicWriteFile_WritesContent(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := []byte("kind: Cluster\nname: test\n")

	err := fsutil.AtomicWriteFile(path, content, 0o600)
	require.NoError(t, err)

	got, readErr := os.ReadFile(path) //nolint:gosec // test file
	require.NoError(t, readErr)
	assert.Equal(t, content, got)
	assertNoTempLeftover(t, dir)
}

// TestAtomicWriteFile_EmptyData verifies a zero-length write produces an empty
// file rather than an error.
func TestAtomicWriteFile_EmptyData(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "empty")

	err := fsutil.AtomicWriteFile(path, []byte{}, 0o600)
	require.NoError(t, err)

	got, readErr := os.ReadFile(path) //nolint:gosec // test file
	require.NoError(t, readErr)
	assert.Empty(t, got)
	assertNoTempLeftover(t, dir)
}

// TestAtomicWriteFile_AppliesPermissions verifies the target file ends up with
// the requested mode. Skipped on Windows, where Unix permission bits are not
// modelled the same way.
func TestAtomicWriteFile_AppliesPermissions(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == windowsGOOS {
		t.Skip("Unix permission bits are not modelled on Windows")
	}

	tests := []struct {
		name string
		perm os.FileMode
	}{
		{name: "private 0600", perm: 0o600},
		{name: "world readable 0644", perm: 0o644},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			path := filepath.Join(dir, "perm-check")

			err := fsutil.AtomicWriteFile(path, []byte("data"), testCase.perm)
			require.NoError(t, err)

			info, statErr := os.Stat(path)
			require.NoError(t, statErr)
			assert.Equal(t, testCase.perm, info.Mode().Perm())
		})
	}
}

// TestAtomicWriteFile_OverwritesAtomically verifies an existing file is replaced
// with the new content, exercising the all-or-nothing rename over a live target.
func TestAtomicWriteFile_OverwritesAtomically(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "kubeconfig")
	require.NoError(t, os.WriteFile(path, []byte("old contents"), 0o600))

	newContent := []byte("new contents")
	err := fsutil.AtomicWriteFile(path, newContent, 0o600)
	require.NoError(t, err)

	got, readErr := os.ReadFile(path) //nolint:gosec // test file
	require.NoError(t, readErr)
	assert.Equal(t, newContent, got)
	assertNoTempLeftover(t, dir)
}

// TestAtomicWriteFile_CreateTempError verifies a missing parent directory
// surfaces as a wrapped create-temp error and writes nothing.
func TestAtomicWriteFile_CreateTempError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "does-not-exist", "config.yaml")

	err := fsutil.AtomicWriteFile(path, []byte("data"), 0o600)
	require.ErrorContains(t, err, "create temp file")

	_, statErr := os.Stat(path)
	assert.True(t, os.IsNotExist(statErr), "no target file should be created on failure")
}

// TestAtomicWriteFile_RenameError verifies a rename failure (target path is an
// existing directory) is surfaced as a wrapped rename error and the staging
// temp file is still cleaned up. Skipped on Windows, where the function takes a
// different remove-and-retry branch for an existing destination.
func TestAtomicWriteFile_RenameError(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == windowsGOOS {
		t.Skip("Windows takes the remove-and-retry rename branch for an existing destination")
	}

	dir := t.TempDir()
	// The target path is an existing directory; renaming a file onto it fails.
	path := filepath.Join(dir, "target-dir")
	require.NoError(t, os.Mkdir(path, 0o750))

	err := fsutil.AtomicWriteFile(path, []byte("data"), 0o600)
	require.ErrorContains(t, err, "rename temp file")
	assertNoTempLeftover(t, dir)
}
