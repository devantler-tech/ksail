package environment_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	"github.com/devantler-tech/ksail/v7/pkg/svc/environment"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeRemoveFixture materialises a declared environment (root config + overlay
// with a nested file) plus the shared base overlay under a fresh temp repo.
func writeRemoveFixture(t *testing.T) string {
	t.Helper()

	repoRoot := t.TempDir()
	files := map[string]string{
		"ksail.staging.yaml":                      "kind: Cluster\n",
		"k8s/clusters/staging/kustomization.yaml": "resources: []\n",
		"k8s/clusters/staging/patches/patch.yaml": "data: {}\n",
		"k8s/clusters/base/kustomization.yaml":    "resources: []\n",
	}

	for rel, content := range files {
		abs := filepath.Join(repoRoot, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(abs), 0o750))
		require.NoError(t, os.WriteFile(abs, []byte(content), 0o600))
	}

	return repoRoot
}

func TestRemoveEnvironmentConfig_RemovesFile(t *testing.T) {
	t.Parallel()

	repoRoot := writeRemoveFixture(t)

	err := environment.RemoveEnvironmentConfig(repoRoot, "ksail.staging.yaml")
	require.NoError(t, err)

	_, statErr := os.Stat(filepath.Join(repoRoot, "ksail.staging.yaml"))
	require.ErrorIs(t, statErr, os.ErrNotExist)
}

func TestRemoveEnvironmentConfig_MissingFile(t *testing.T) {
	t.Parallel()

	repoRoot := writeRemoveFixture(t)

	err := environment.RemoveEnvironmentConfig(repoRoot, "ksail.nosuch.yaml")
	require.ErrorIs(t, err, environment.ErrEnvironmentConfigMissing)
}

func TestRemoveEnvironmentConfig_RejectsEscape(t *testing.T) {
	t.Parallel()

	repoRoot := writeRemoveFixture(t)

	// A config path escaping the repository root must be rejected, not deleted.
	outside := filepath.Join(repoRoot, "..", "escape.yaml")
	require.NoError(t, os.WriteFile(outside, []byte("x"), 0o600))

	t.Cleanup(func() { _ = os.Remove(outside) })

	err := environment.RemoveEnvironmentConfig(repoRoot, "../escape.yaml")
	require.ErrorIs(t, err, environment.ErrEnvironmentConfigMissing)

	_, statErr := os.Stat(outside)
	require.NoError(t, statErr)
}

func TestRemoveOverlay_RemovesDirectory(t *testing.T) {
	t.Parallel()

	repoRoot := writeRemoveFixture(t)

	removed, err := environment.RemoveOverlay(repoRoot, "k8s/clusters/staging")
	require.NoError(t, err)
	assert.True(t, removed)

	_, statErr := os.Stat(filepath.Join(repoRoot, "k8s", "clusters", "staging"))
	require.ErrorIs(t, statErr, os.ErrNotExist)

	// The shared base overlay next to it is untouched.
	_, statErr = os.Stat(filepath.Join(repoRoot, "k8s", "clusters", "base"))
	require.NoError(t, statErr)
}

func TestRemoveOverlay_MissingIsNotAnError(t *testing.T) {
	t.Parallel()

	repoRoot := writeRemoveFixture(t)

	removed, err := environment.RemoveOverlay(repoRoot, "k8s/clusters/nosuch")
	require.NoError(t, err)
	assert.False(t, removed)
}

func TestRemoveOverlay_RefusesSharedBase(t *testing.T) {
	t.Parallel()

	repoRoot := writeRemoveFixture(t)

	_, err := environment.RemoveOverlay(repoRoot, "k8s/clusters/base")
	require.ErrorIs(t, err, environment.ErrSharedBaseOverlay)

	_, statErr := os.Stat(filepath.Join(repoRoot, "k8s", "clusters", "base"))
	require.NoError(t, statErr)
}

func TestRemoveOverlay_RejectsEscape(t *testing.T) {
	t.Parallel()

	repoRoot := writeRemoveFixture(t)

	_, err := environment.RemoveOverlay(repoRoot, "../outside")
	require.ErrorIs(t, err, fsutil.ErrPathOutsideBase)
}

func TestRemoveOverlay_SymlinkRemovesLinkOnly(t *testing.T) {
	t.Parallel()

	repoRoot := writeRemoveFixture(t)

	// An overlay that is a symlink pointing outside the repo: only the link goes.
	target := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(target, "keep.yaml"), []byte("x"), 0o600))

	link := filepath.Join(repoRoot, "k8s", "clusters", "linked")
	require.NoError(t, os.Symlink(target, link))

	removed, err := environment.RemoveOverlay(repoRoot, "k8s/clusters/linked")
	require.NoError(t, err)
	assert.True(t, removed)

	_, statErr := os.Lstat(link)
	require.ErrorIs(t, statErr, os.ErrNotExist)

	_, statErr = os.Stat(filepath.Join(target, "keep.yaml"))
	require.NoError(t, statErr)
}
