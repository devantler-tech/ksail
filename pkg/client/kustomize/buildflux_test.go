package kustomize_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/client/kustomize"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/kustomize/api/types"
)

const webConfigMap = "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: web\ndata:\n  key: value\n"

// writeFiles writes files into a fresh folder and returns it.
func writeFiles(t *testing.T, files map[string]string) string {
	t.Helper()

	dir := t.TempDir()
	for name, content := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600))
	}

	return dir
}

// A folder without a kustomization file is built from the one Flux generates,
// with the Kustomization's targetNamespace applied, and nothing is written.
func TestBuildFluxGeneratesMissingKustomization(t *testing.T) {
	t.Parallel()

	dir := writeFiles(t, map[string]string{"configmap.yaml": webConfigMap})

	out, err := kustomize.NewClient().BuildFlux(
		t.Context(), dir, kustomize.FluxComposition{TargetNamespace: "tenant-a"},
	)
	require.NoError(t, err)
	assert.Contains(t, out.String(), "name: web")
	assert.Contains(t, out.String(), "namespace: tenant-a")

	_, statErr := os.Stat(filepath.Join(dir, "kustomization.yaml"))
	assert.ErrorIs(t, statErr, os.ErrNotExist, "the generated kustomization stays in memory")
}

// Flux patches join the folder's own kustomization and run in that one build,
// so a patch selecting a label the folder's labels transformer adds does not
// match, as in kustomize-controller (devantler-tech/ksail#8560).
func TestBuildFluxPatchesRunInsideTheFolderBuild(t *testing.T) {
	t.Parallel()

	dir := writeFiles(t, map[string]string{
		"configmap.yaml": webConfigMap,
		"kustomization.yaml": "apiVersion: kustomize.config.k8s.io/v1beta1\nkind: Kustomization\n" +
			"resources:\n  - configmap.yaml\nlabels:\n  - pairs:\n      app: web\n",
	})

	patch := types.Patch{
		Target: &types.Selector{LabelSelector: "app=web", Kind: "ConfigMap"},
		Patch:  "- op: add\n  path: /data/patched\n  value: \"yes\"\n",
	}

	out, err := kustomize.NewClient().BuildFlux(
		t.Context(), dir, kustomize.FluxComposition{Patches: []types.Patch{patch}},
	)
	require.NoError(t, err)
	assert.Contains(t, out.String(), "app: web", "the folder's own labels still apply")
	assert.NotContains(t, out.String(), "patched", "the label is added after the patch runs")

	// Control: a patch selecting by name does apply.
	patch.Target = &types.Selector{Kind: "ConfigMap", Name: "web"}

	out, err = kustomize.NewClient().BuildFlux(
		t.Context(), dir, kustomize.FluxComposition{Patches: []types.Patch{patch}},
	)
	require.NoError(t, err)
	assert.Contains(t, out.String(), "patched")

	kustFile := filepath.Join(dir, "kustomization.yaml")
	original, err := os.ReadFile(kustFile) //nolint:gosec // test-owned temp folder
	require.NoError(t, err)
	assert.NotContains(t, string(original), "patches", "the folder's file is never rewritten")
}

// A relative folder builds like its absolute path, with and without a
// kustomization file, so the in-memory kustomization is still the one used.
//
//nolint:paralleltest // t.Chdir changes the process working directory
func TestBuildFluxRelativeFolder(t *testing.T) {
	root := t.TempDir()

	generated := filepath.Join(root, "generated")
	listed := filepath.Join(root, "listed")

	require.NoError(t, os.MkdirAll(generated, 0o750))
	require.NoError(t, os.MkdirAll(listed, 0o750))
	require.NoError(
		t,
		os.WriteFile(filepath.Join(generated, "configmap.yaml"), []byte(webConfigMap), 0o600),
	)
	require.NoError(
		t,
		os.WriteFile(filepath.Join(listed, "configmap.yaml"), []byte(webConfigMap), 0o600),
	)
	require.NoError(t, os.WriteFile(filepath.Join(listed, "kustomization.yaml"), []byte(
		"apiVersion: kustomize.config.k8s.io/v1beta1\nkind: Kustomization\nresources:\n  - configmap.yaml\n",
	), 0o600))

	t.Chdir(root)

	for _, dir := range []string{"generated", "./listed"} {
		out, err := kustomize.NewClient().BuildFlux(
			t.Context(), dir, kustomize.FluxComposition{TargetNamespace: "tenant-a"},
		)
		require.NoError(t, err, dir)
		assert.Contains(t, out.String(), "namespace: tenant-a", dir)
	}
}
