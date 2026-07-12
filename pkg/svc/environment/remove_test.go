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

// TestRemoveEnvironmentConfig_RemovesFile pins the happy path: removing a
// declared environment config deletes its ksail.<name>.yaml file.
func TestRemoveEnvironmentConfig_RemovesFile(t *testing.T) {
	t.Parallel()

	repoRoot := writeRemoveFixture(t)

	err := environment.RemoveEnvironmentConfig(repoRoot, "ksail.staging.yaml")
	require.NoError(t, err)

	_, statErr := os.Stat(filepath.Join(repoRoot, "ksail.staging.yaml"))
	require.ErrorIs(t, statErr, os.ErrNotExist)
}

// TestRemoveEnvironmentConfig_MissingFile pins that removing a config that was
// never declared fails with ErrEnvironmentConfigMissing.
func TestRemoveEnvironmentConfig_MissingFile(t *testing.T) {
	t.Parallel()

	repoRoot := writeRemoveFixture(t)

	err := environment.RemoveEnvironmentConfig(repoRoot, "ksail.nosuch.yaml")
	require.ErrorIs(t, err, environment.ErrEnvironmentConfigMissing)
}

// TestRemoveEnvironmentConfig_RejectsEscape pins that a config path escaping
// the repository root is refused and the outside file is left untouched.
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

// TestRemoveOverlay_RemovesDirectory pins the happy path: the environment's
// overlay directory is deleted recursively while the sibling shared base
// overlay survives.
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

// TestRemoveOverlay_MissingIsNotAnError pins that a nonexistent overlay is not
// an error: RemoveOverlay reports removed=false and succeeds.
func TestRemoveOverlay_MissingIsNotAnError(t *testing.T) {
	t.Parallel()

	repoRoot := writeRemoveFixture(t)

	removed, err := environment.RemoveOverlay(repoRoot, "k8s/clusters/nosuch")
	require.NoError(t, err)
	assert.False(t, removed)
}

// TestRemoveOverlay_RefusesRootEquivalentPaths pins the guard against a
// root-equivalent overlay path reaching the recursive delete: containedPath
// accepts the repository root as "contained", so without the refusal
// `env rm --purge` with an empty/"."/"clusters/.." path would delete the
// whole workspace.
func TestRemoveOverlay_RefusesRootEquivalentPaths(t *testing.T) {
	t.Parallel()

	repoRoot := writeRemoveFixture(t)

	for _, overlay := range []string{"", ".", "./", "/", "k8s/clusters/../.."} {
		_, err := environment.RemoveOverlay(repoRoot, overlay)
		require.ErrorIs(t, err, environment.ErrRootEquivalentOverlay, "path %q", overlay)
	}

	// The fixture tree is untouched by every refused call.
	_, statErr := os.Stat(filepath.Join(repoRoot, "k8s", "clusters", "base"))
	require.NoError(t, statErr)
}

// TestRemoveOverlay_RefusesSharedBase pins that the shared clusters/base
// overlay is refused with ErrSharedBaseOverlay and left in place.
func TestRemoveOverlay_RefusesSharedBase(t *testing.T) {
	t.Parallel()

	repoRoot := writeRemoveFixture(t)

	_, err := environment.RemoveOverlay(repoRoot, "k8s/clusters/base")
	require.ErrorIs(t, err, environment.ErrSharedBaseOverlay)

	_, statErr := os.Stat(filepath.Join(repoRoot, "k8s", "clusters", "base"))
	require.NoError(t, statErr)
}

// TestRemoveOverlay_RejectsEscape pins that an overlay path escaping the
// repository root is refused with fsutil.ErrPathOutsideBase.
func TestRemoveOverlay_RejectsEscape(t *testing.T) {
	t.Parallel()

	repoRoot := writeRemoveFixture(t)

	_, err := environment.RemoveOverlay(repoRoot, "../outside")
	require.ErrorIs(t, err, fsutil.ErrPathOutsideBase)
}

// TestRemoveOverlay_SymlinkRemovesLinkOnly pins that an overlay which is itself
// a symlink only has the link removed — the outside target directory survives.
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

// TestRemoveOverlay_RejectsSymlinkedParentEscape pins that a parent segment
// symlinked to a directory outside the repository is refused — the lexical
// containment check alone must not let the delete traverse the link.
func TestRemoveOverlay_RejectsSymlinkedParentEscape(t *testing.T) {
	t.Parallel()

	// The lexical containment check passes for k8s/clusters/prod, but
	// k8s/clusters is a symlink to a directory OUTSIDE the repository: the
	// recursive delete must refuse rather than traverse the link and delete
	// the outside target.
	repoRoot := t.TempDir()
	outside := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(outside, "prod"), 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(repoRoot, "k8s"), 0o750))
	require.NoError(t, os.Symlink(outside, filepath.Join(repoRoot, "k8s", "clusters")))

	removed, err := environment.RemoveOverlay(repoRoot, "k8s/clusters/prod")
	require.ErrorIs(t, err, fsutil.ErrPathOutsideBase)
	assert.False(t, removed)

	_, statErr := os.Stat(filepath.Join(outside, "prod"))
	require.NoError(t, statErr, "the outside target must survive")
}

// TestRemoveOverlay_AllowsInRepoSymlinkedParent pins that a symlinked parent
// segment which still resolves inside the repository is treated as legitimate
// layout: the overlay is removed, not refused as an escape.
func TestRemoveOverlay_AllowsInRepoSymlinkedParent(t *testing.T) {
	t.Parallel()

	// A symlinked intermediate segment that still RESOLVES inside the
	// repository is legitimate repo layout, not an escape.
	repoRoot := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(repoRoot, "real", "clusters", "prod"), 0o750))
	require.NoError(t, os.Symlink(
		filepath.Join(repoRoot, "real"),
		filepath.Join(repoRoot, "k8s"),
	))

	removed, err := environment.RemoveOverlay(repoRoot, "k8s/clusters/prod")
	require.NoError(t, err)
	assert.True(t, removed)

	_, statErr := os.Stat(filepath.Join(repoRoot, "real", "clusters", "prod"))
	require.ErrorIs(t, statErr, os.ErrNotExist)
}

// TestRemoveOverlay_RefusesBaseAliasedThroughSymlink pins that aliasing the
// shared base overlay through a symlinked parent still trips the base refusal:
// the resolved-path check catches what the lexical check would miss.
func TestRemoveOverlay_RefusesBaseAliasedThroughSymlink(t *testing.T) {
	t.Parallel()

	// A parent symlink can alias another name onto the shared clusters/base
	// overlay, sidestepping the lexical base refusal; the resolved-path check
	// must still refuse it.
	repoRoot := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(repoRoot, "k8s", "clusters", "base"), 0o750))
	require.NoError(t, os.Symlink(
		filepath.Join(repoRoot, "k8s", "clusters", "base"),
		filepath.Join(repoRoot, "k8s", "clusters", "prod"),
	))

	// The overlay itself being a symlink only drops the link — that path is
	// already safe. Alias a PARENT instead: shadow/ -> k8s/clusters, then
	// delete shadow/base... which IS clusters/base. Use a distinct leaf name
	// pointing at the base directory through a real parent chain.
	require.NoError(t, os.RemoveAll(filepath.Join(repoRoot, "k8s", "clusters", "prod")))
	require.NoError(t, os.Symlink(
		filepath.Join(repoRoot, "k8s", "clusters"),
		filepath.Join(repoRoot, "shadow"),
	))

	removed, err := environment.RemoveOverlay(repoRoot, "shadow/base")
	require.ErrorIs(t, err, environment.ErrSharedBaseOverlay)
	assert.False(t, removed)

	_, statErr := os.Stat(filepath.Join(repoRoot, "k8s", "clusters", "base"))
	require.NoError(t, statErr, "the shared base overlay must survive")
}

// TestRemoveEnvironmentConfig_RejectsSymlinkTarget pins that a config path
// whose final component is a symlink is refused as missing — only a regular
// file counts as a declared config, and the link's outside target survives.
func TestRemoveEnvironmentConfig_RejectsSymlinkTarget(t *testing.T) {
	t.Parallel()

	// The Lstat IsRegular check refuses a config path whose final component is
	// a symlink: the environment's declared config must be a real file, and the
	// link's outside target must never be touched.
	repoRoot := writeRemoveFixture(t)
	outside := filepath.Join(t.TempDir(), "outside.yaml")
	require.NoError(t, os.WriteFile(outside, []byte("x"), 0o600))

	link := filepath.Join(repoRoot, "ksail.linked.yaml")
	require.NoError(t, os.Symlink(outside, link))

	err := environment.RemoveEnvironmentConfig(repoRoot, "ksail.linked.yaml")
	require.ErrorIs(t, err, environment.ErrEnvironmentConfigMissing)

	_, statErr := os.Stat(outside)
	require.NoError(t, statErr)
}

// TestRemoveOverlay_RefusesRootCollapsedThroughSymlink pins that an overlay
// whose RESOLVED path is the repository root itself (an intermediate symlink
// pointing at the workspace's parent plus an environment named like the
// checkout) trips the root-equivalence refusal on the canonical path — the
// lexical guard alone cannot see it, and RemoveAll would wipe the workspace.
func TestRemoveOverlay_RefusesRootCollapsedThroughSymlink(t *testing.T) {
	t.Parallel()

	parent := t.TempDir()
	repoRoot := filepath.Join(parent, "ksail")
	require.NoError(t, os.MkdirAll(filepath.Join(repoRoot, "k8s"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(repoRoot, "keep.yaml"), []byte("x"), 0o600))
	require.NoError(t, os.Symlink(parent, filepath.Join(repoRoot, "k8s", "clusters")))

	removed, err := environment.RemoveOverlay(repoRoot, "k8s/clusters/ksail")
	require.ErrorIs(t, err, environment.ErrRootEquivalentOverlay)
	assert.False(t, removed)

	_, statErr := os.Stat(filepath.Join(repoRoot, "keep.yaml"))
	require.NoError(t, statErr, "the workspace must survive")
}

// TestRemoveOverlay_RefusesNonDirectoryOverlay pins that a regular file at the
// overlay path is refused: an overlay is by definition a directory, so a file
// there is a malformed workspace to surface, not content to delete.
func TestRemoveOverlay_RefusesNonDirectoryOverlay(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(repoRoot, "k8s", "clusters"), 0o750))
	require.NoError(t, os.WriteFile(
		filepath.Join(repoRoot, "k8s", "clusters", "prod"), []byte("not a dir"), 0o600))

	removed, err := environment.RemoveOverlay(repoRoot, "k8s/clusters/prod")
	require.ErrorIs(t, err, environment.ErrNonDirectoryOverlay)
	assert.False(t, removed)

	_, statErr := os.Stat(filepath.Join(repoRoot, "k8s", "clusters", "prod"))
	require.NoError(t, statErr, "the file must survive")
}
