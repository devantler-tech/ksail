package fsutil_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const sentinelContent = "SENTINEL"

// readBack reads a file the test itself created under its own t.TempDir().
func readBack(t *testing.T, path string) string {
	t.Helper()

	//nolint:gosec // G304: reads a file just written under the test's own t.TempDir().
	data, err := os.ReadFile(path)
	require.NoError(t, err)

	return string(data)
}

// requireSymlinkSupport skips the calling test when the filesystem cannot create
// symlinks (notably an unprivileged Windows runner), so a platform limitation is
// reported as a skip rather than a failure. The probe runs in its own directory so
// it cannot leave artefacts in the directory under test.
func requireSymlinkSupport(t *testing.T) {
	t.Helper()

	probe := t.TempDir()
	target := filepath.Join(probe, "target")
	require.NoError(t, os.WriteFile(target, []byte("probe"), 0o600))

	err := os.Symlink(target, filepath.Join(probe, "link"))
	if err != nil {
		t.Skipf("symlinks not supported: %v", err)
	}
}

// TestTryWriteFileWithin_RefusesEscapeThroughSwappedParent is the regression proof
// for the post-validation symlink swap: the parent directory is a real directory
// when a caller validates it and a symlink pointing outside the base by the time
// the write happens. The write must fail and the file outside the base must keep
// its original content.
func TestTryWriteFileWithin_RefusesEscapeThroughSwappedParent(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	outside := t.TempDir()

	requireSymlinkSupport(t)

	sentinel := filepath.Join(outside, "sentinel.txt")
	require.NoError(t, os.WriteFile(sentinel, []byte(sentinelContent), 0o600))

	// The state a caller's containment check would have observed: a real directory.
	parent := filepath.Join(base, "nested")
	require.NoError(t, os.Mkdir(parent, 0o750))

	// The swap an attacker performs between that check and the write.
	require.NoError(t, os.Remove(parent))
	require.NoError(t, os.Symlink(outside, parent))

	wrote, err := fsutil.TryWriteFileWithin(base, "nested/sentinel.txt", "PWNED", true)

	require.Error(t, err)
	assert.False(t, wrote)

	assert.Equal(t, sentinelContent, readBack(t, sentinel),
		"a write through a swapped parent must not reach outside the base directory")
}

// TestTryWriteFileWithin_RefusesEscapeThroughSymlinkedDestination covers the final
// path component pointing outside the base, rather than a parent.
func TestTryWriteFileWithin_RefusesEscapeThroughSymlinkedDestination(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	outside := t.TempDir()

	requireSymlinkSupport(t)

	sentinel := filepath.Join(outside, "sentinel.txt")
	require.NoError(t, os.WriteFile(sentinel, []byte(sentinelContent), 0o600))
	require.NoError(t, os.Symlink(sentinel, filepath.Join(base, "link.txt")))

	wrote, err := fsutil.TryWriteFileWithin(base, "link.txt", "PWNED", true)

	require.Error(t, err)
	assert.False(t, wrote)

	assert.Equal(t, sentinelContent, readBack(t, sentinel))
}

// TestTryWriteFileWithin_WritesInsideBase is the control: the guard above must not
// pass by refusing everything.
func TestTryWriteFileWithin_WritesInsideBase(t *testing.T) {
	t.Parallel()

	base := t.TempDir()

	wrote, err := fsutil.TryWriteFileWithin(base, "nested/deep/file.txt", testContent, false)

	require.NoError(t, err)
	assert.True(t, wrote)

	assert.Equal(t, testContent, readBack(t, filepath.Join(base, "nested", "deep", "file.txt")))
}

func TestTryWriteFileWithin_SkipsExistingWithoutForce(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	dest := filepath.Join(base, "file.txt")
	require.NoError(t, os.WriteFile(dest, []byte(originalContent), 0o600))

	wrote, err := fsutil.TryWriteFileWithin(base, "file.txt", testContent, false)

	require.NoError(t, err)
	assert.False(t, wrote)

	assert.Equal(t, originalContent, readBack(t, dest), "an existing file must be preserved without force")
}

func TestTryWriteFileWithin_OverwritesExistingWithForce(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	dest := filepath.Join(base, "file.txt")
	require.NoError(t, os.WriteFile(dest, []byte(originalContent), 0o600))

	wrote, err := fsutil.TryWriteFileWithin(base, "file.txt", testContent, true)

	require.NoError(t, err)
	assert.True(t, wrote)

	assert.Equal(t, testContent, readBack(t, dest))
}

func TestTryWriteFileWithin_RejectsInvalidPaths(t *testing.T) {
	t.Parallel()

	base := t.TempDir()

	testCases := []struct {
		name     string
		basePath string
		relPath  string
		wantErr  error
	}{
		{name: "empty base", basePath: "", relPath: "file.txt", wantErr: fsutil.ErrBasePath},
		{name: "empty rel", basePath: base, relPath: "", wantErr: fsutil.ErrEmptyOutputPath},
		{name: "parent escape", basePath: base, relPath: "../escape.txt", wantErr: fsutil.ErrPathOutsideBase},
		{
			name:     "nested parent escape",
			basePath: base,
			relPath:  "nested/../../escape.txt",
			wantErr:  fsutil.ErrPathOutsideBase,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			wrote, err := fsutil.TryWriteFileWithin(
				testCase.basePath, testCase.relPath, testContent, true,
			)

			require.ErrorIs(t, err, testCase.wantErr)
			assert.False(t, wrote)
		})
	}
}
