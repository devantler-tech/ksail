package kustomize_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/client/kustomize"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

// fluxSpec is a Flux Kustomization object with spec.
func fluxSpec(spec map[string]any) map[string]any {
	return map[string]any{
		"apiVersion": "kustomize.toolkit.fluxcd.io/v1",
		"kind":       "Kustomization",
		"metadata":   map[string]any{"name": "apps", "namespace": "flux-system"},
		"spec":       spec,
	}
}

// A folder without a kustomization file is built from the one Flux generates,
// with the Kustomization's targetNamespace applied, and nothing is written.
func TestBuildFluxGeneratesMissingKustomization(t *testing.T) {
	t.Parallel()

	dir := writeFiles(t, map[string]string{"configmap.yaml": webConfigMap})

	out, err := kustomize.NewClient().BuildFlux(
		t.Context(), dir, dir, fluxSpec(map[string]any{"targetNamespace": "tenant-a"}),
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

	patch := map[string]any{
		"target": map[string]any{"kind": "ConfigMap", "labelSelector": "app=web"},
		"patch":  "- op: add\n  path: /data/patched\n  value: \"yes\"\n",
	}

	out, err := kustomize.NewClient().BuildFlux(
		t.Context(), dir, dir, fluxSpec(map[string]any{"patches": []any{patch}}),
	)
	require.NoError(t, err)
	assert.Contains(t, out.String(), "app: web", "the folder's own labels still apply")
	assert.NotContains(t, out.String(), "patched", "the label is added after the patch runs")

	// Control: a patch selecting by name does apply.
	patch["target"] = map[string]any{"kind": "ConfigMap", "name": "web"}

	out, err = kustomize.NewClient().BuildFlux(
		t.Context(), dir, dir, fluxSpec(map[string]any{"patches": []any{patch}}),
	)
	require.NoError(t, err)
	assert.Contains(t, out.String(), "patched")

	original, err := os.ReadFile( //nolint:gosec // test-owned temp folder
		filepath.Join(dir, "kustomization.yaml"),
	)
	require.NoError(t, err)
	assert.NotContains(t, string(original), "patches", "the folder's file is never rewritten")
}

// A relative folder such as "." is resolved to the same absolute path kustomize
// builds, so the merged kustomization is still served and the Flux spec applies
// (devantler-tech/ksail#8560).
//
//nolint:paralleltest // t.Chdir changes the process working directory.
func TestBuildFluxResolvesRelativeFolder(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"configmap.yaml": webConfigMap,
		"kustomization.yaml": "apiVersion: kustomize.config.k8s.io/v1beta1\nkind: Kustomization\n" +
			"resources:\n  - configmap.yaml\n",
	})
	t.Chdir(dir)

	out, err := kustomize.NewClient().BuildFlux(
		t.Context(), ".", ".", fluxSpec(map[string]any{"targetNamespace": "tenant-a"}),
	)
	require.NoError(t, err)
	assert.Contains(t, out.String(), "namespace: tenant-a")
}

// A component outside the source root fails, as kustomize-controller confines
// generation and the build to the source; a sibling inside the root still loads.
func TestBuildFluxConfinesTheBuildToTheSourceRoot(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	outside := t.TempDir()

	for _, component := range []string{filepath.Join(root, "shared"), outside} {
		require.NoError(t, os.MkdirAll(component, 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(component, "kustomization.yaml"),
			[]byte("apiVersion: kustomize.config.k8s.io/v1alpha1\nkind: Component\n"+
				"commonAnnotations:\n  component: applied\n"), 0o600))
	}

	dir := filepath.Join(root, "apps")
	require.NoError(t, os.MkdirAll(dir, 0o750))
	require.NoError(
		t,
		os.WriteFile(filepath.Join(dir, "configmap.yaml"), []byte(webConfigMap), 0o600),
	)

	inside, err := filepath.Rel(dir, filepath.Join(root, "shared"))
	require.NoError(t, err)

	out, err := kustomize.NewClient().BuildFlux(
		t.Context(), root, dir, fluxSpec(map[string]any{"components": []any{inside}}),
	)
	require.NoError(t, err)
	assert.Contains(t, out.String(), "component: applied", "a component inside the source loads")

	escaping, err := filepath.Rel(dir, outside)
	require.NoError(t, err)

	_, err = kustomize.NewClient().BuildFlux(
		t.Context(), root, dir, fluxSpec(map[string]any{"components": []any{escaping}}),
	)
	require.Error(t, err, "a component outside the source root is refused")
}

// A canceled context stops the build before any work starts.
func TestBuildFluxObservesCancellation(t *testing.T) {
	t.Parallel()

	dir := writeFiles(t, map[string]string{"configmap.yaml": webConfigMap})

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := kustomize.NewClient().BuildFlux(ctx, dir, dir, fluxSpec(map[string]any{}))
	require.ErrorIs(t, err, context.Canceled)
}
