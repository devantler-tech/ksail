package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCanonicalizePathsResolvesSymlinks(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("symlink permissions vary on Windows runners")
	}

	realRoot := t.TempDir()
	linkDir := t.TempDir()
	rootLink := filepath.Join(linkDir, "repo")
	feedPath := filepath.Join(realRoot, "feed.rss")
	feedLink := filepath.Join(linkDir, "feed.rss")
	require.NoError(t, os.WriteFile(feedPath, []byte("fixture"), 0o600))
	require.NoError(t, os.Symlink(realRoot, rootLink))
	require.NoError(t, os.Symlink(feedPath, feedLink))

	root, feed, err := canonicalizePaths(rootLink, feedLink)
	require.NoError(t, err)
	expectedRoot, err := filepath.EvalSymlinks(realRoot)
	require.NoError(t, err)
	expectedFeed, err := filepath.EvalSymlinks(feedPath)
	require.NoError(t, err)
	assert.Equal(t, expectedRoot, root)
	assert.Equal(t, expectedFeed, feed)
}

func TestCanonicalizePathsPreservesEmptyFeed(t *testing.T) {
	t.Parallel()

	realRoot := t.TempDir()
	root, feed, err := canonicalizePaths(realRoot, "")
	require.NoError(t, err)
	expectedRoot, err := filepath.EvalSymlinks(realRoot)
	require.NoError(t, err)
	assert.Equal(t, expectedRoot, root)
	assert.Empty(t, feed)
}
