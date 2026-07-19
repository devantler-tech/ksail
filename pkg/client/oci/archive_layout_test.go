package oci_test

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/client/argocd"
	"github.com/devantler-tech/ksail/v7/pkg/client/oci"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeWorkloadTree lays out a workload source directory that mirrors the shape a
// default `ksail project init` produces: a non-root source directory ("k8s") whose
// own root holds the entrypoint kustomization.
//
// It returns the absolute source root, which is what `ksail workload push` passes
// to the archive builder.
func writeWorkloadTree(t *testing.T) string {
	t.Helper()

	root := filepath.Join(t.TempDir(), "k8s")
	nested := filepath.Join(root, "clusters", "local")

	require.NoError(t, os.MkdirAll(nested, 0o750))
	require.NoError(t, os.WriteFile(
		filepath.Join(root, "kustomization.yaml"),
		[]byte("resources:\n  - clusters/local\n"),
		0o600,
	))
	require.NoError(t, os.WriteFile(
		filepath.Join(nested, "kustomization.yaml"),
		[]byte("resources: []\n"),
		0o600,
	))

	return root
}

// archiveEntries builds the manifest layer and returns every tar entry name it
// contains.
func archiveEntries(t *testing.T, root string) []string {
	t.Helper()

	files, err := oci.CollectManifestFiles(root)
	require.NoError(t, err)
	require.NotEmpty(t, files, "fixture must contain manifests")

	layer, err := oci.NewManifestLayer(root, files)
	require.NoError(t, err)

	compressed, err := layer.Compressed()
	require.NoError(t, err)

	defer func() { _ = compressed.Close() }()

	gzipReader, err := gzip.NewReader(compressed)
	require.NoError(t, err)

	defer func() { _ = gzipReader.Close() }()

	var entries []string

	tarReader := tar.NewReader(gzipReader)

	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}

		require.NoError(t, err)

		entries = append(entries, header.Name)
	}

	return entries
}

// TestManifestLayerServesArgoCDSourcePath is the regression guard for #6284.
//
// Argo CD resolves `spec.source.path` relative to the root of the expanded OCI
// archive, and KSail always generates the root Application with the default path.
// If the archive carries no manifest at that path, Argo CD renders nothing and
// still reports the Application Synced and Healthy — a silent no-op.
//
// So the archive must serve the exact path the generated Application points at.
func TestManifestLayerServesArgoCDSourcePath(t *testing.T) {
	t.Parallel()

	root := writeWorkloadTree(t)
	entries := archiveEntries(t, root)

	// argocd.DefaultSourcePath is "." — the archive root — so the entrypoint
	// kustomization must be reachable there, unprefixed.
	wantEntry := filepath.ToSlash(
		filepath.Join(filepath.Clean(argocd.DefaultSourcePath), "kustomization.yaml"),
	)

	assert.Contains(t, entries, wantEntry,
		"archive must contain a manifest at the Application's source path %q; "+
			"entries were %v", argocd.DefaultSourcePath, entries,
	)
}

// archiveEntryContents reads every tar entry as name -> content, and fails the test if any
// name appears twice. Duplicate names are the defect under test: tar permits them, and an
// extractor silently keeps whichever came last.
func archiveEntryContents(t *testing.T, root string) map[string]string {
	t.Helper()

	files, err := oci.CollectManifestFiles(root)
	require.NoError(t, err)

	layer, err := oci.NewManifestLayer(root, files)
	require.NoError(t, err)

	compressed, err := layer.Compressed()
	require.NoError(t, err)

	defer func() { _ = compressed.Close() }()

	gzipReader, err := gzip.NewReader(compressed)
	require.NoError(t, err)

	defer func() { _ = gzipReader.Close() }()

	contents := make(map[string]string)
	tarReader := tar.NewReader(gzipReader)

	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}

		require.NoError(t, err)

		_, seen := contents[header.Name]
		require.False(t, seen,
			"archive contains two entries named %q; extraction keeps only one of them",
			header.Name)

		body, err := io.ReadAll(tarReader)
		require.NoError(t, err)

		contents[header.Name] = string(body)
	}

	return contents
}

// TestManifestLayerCompatibilityAliasNeverShadowsRoot covers the collision Codex flagged on #6285.
//
// The compatibility copies are written under a directory named after the source directory. When
// the source tree itself contains a child of that same name — sourceDirectory "k8s" holding a
// nested "k8s/" — the alias of a top-level file and the root entry of the nested file both land
// on "k8s/<name>". Two different files, one tar name.
//
// The root tree is what Argo CD's path "." and Flux's "./" resolve against, so the alias must
// never win: whoever reads "k8s/kustomization.yaml" must get the nested file, not the top-level
// one wearing its name.
func TestManifestLayerCompatibilityAliasNeverShadowsRoot(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "k8s")
	nested := filepath.Join(root, "k8s")

	require.NoError(t, os.MkdirAll(nested, 0o750))

	const (
		topLevelBody = "resources:\n  - k8s\n"
		nestedBody   = "resources: []\n"
	)

	require.NoError(t, os.WriteFile(
		filepath.Join(root, "kustomization.yaml"), []byte(topLevelBody), 0o600))
	require.NoError(t, os.WriteFile(
		filepath.Join(nested, "kustomization.yaml"), []byte(nestedBody), 0o600))

	contents := archiveEntryContents(t, root)

	assert.Equal(t, topLevelBody, contents["kustomization.yaml"],
		"the source root's kustomization must be published at the archive root")

	assert.Equal(t, nestedBody, contents["k8s/kustomization.yaml"],
		"k8s/kustomization.yaml must be the NESTED file's root entry, not a compatibility "+
			"alias of the top-level file shadowing it")
}

// TestManifestLayerPublishesRootAndPrefixedEntries pins the full layout contract.
//
// The root entries are what both consumers resolve against — Flux via the
// FluxInstance sync path (default "./") and Argo CD via the Application source
// path ("."). The prefixed copies are the retained compatibility alias for a
// consumer pointed at "<sourceDirectory>/...".
func TestManifestLayerPublishesRootAndPrefixedEntries(t *testing.T) {
	t.Parallel()

	root := writeWorkloadTree(t)
	entries := archiveEntries(t, root)

	assert.Contains(t, entries, "kustomization.yaml",
		"entrypoint kustomization must be published at the archive root")
	assert.Contains(t, entries, "clusters/local/kustomization.yaml",
		"nested layout must be preserved under the archive root")

	assert.Contains(t, entries, "k8s/kustomization.yaml",
		"compatibility prefix copy must be retained")
	assert.Contains(t, entries, "k8s/clusters/local/kustomization.yaml",
		"compatibility prefix copy must preserve the nested layout")
}
