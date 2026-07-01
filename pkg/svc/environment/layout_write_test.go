package environment_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/fsutil/generator"
	kustomizationgenerator "github.com/devantler-tech/ksail/v7/pkg/fsutil/generator/kustomization"
	yamlgenerator "github.com/devantler-tech/ksail/v7/pkg/fsutil/generator/yaml"
	"github.com/devantler-tech/ksail/v7/pkg/svc/environment"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	ktypes "sigs.k8s.io/kustomize/api/types"
)

// layoutRelPaths returns the two files DeriveMultiClusterLayout("prod") seeds, in
// layout order: the shared base, then the per-environment overlay.
func layoutRelPaths() (string, string) {
	return filepath.Join("clusters", "base", "kustomization.yaml"),
		filepath.Join("clusters", "prod", "kustomization.yaml")
}

// TestWriteMultiClusterLayout_WritesBaseAndOverlay asserts the writer renders the
// derived layout under the source dir with the real generator, creating the nested
// per-environment directory and returning the resolved paths in layout order.
func TestWriteMultiClusterLayout_WritesBaseAndOverlay(t *testing.T) {
	t.Parallel()

	sourceDir := t.TempDir()

	files, err := environment.DeriveMultiClusterLayout("prod")
	require.NoError(t, err)

	written, err := environment.WriteMultiClusterLayout(
		kustomizationgenerator.NewGenerator(), sourceDir, files, false,
	)
	require.NoError(t, err)

	baseRel, overlayRel := layoutRelPaths()
	basePath := filepath.Join(sourceDir, baseRel)
	overlayPath := filepath.Join(sourceDir, overlayRel)
	assert.Equal(t, []string{basePath, overlayPath}, written)

	baseContent := readFile(t, basePath)
	assert.Contains(t, baseContent, "kind: Kustomization")
	assert.Contains(t, baseContent, "apiVersion: kustomize.config.k8s.io/v1beta1")
	assert.NotContains(t, baseContent, "../base")

	// The environment overlay references the shared base via its sibling path, so
	// the two files together form a working multi-cluster tree.
	overlayContent := readFile(t, overlayPath)
	assert.Contains(t, overlayContent, "kind: Kustomization")
	assert.Contains(t, overlayContent, "../base")
}

// TestWriteMultiClusterLayout_ByteConsistentWithGenerator asserts the written
// bytes are exactly what the scaffolder's kustomization generator produces for the
// same model — the byte-consistency guarantee the LayoutFile doc promises.
func TestWriteMultiClusterLayout_ByteConsistentWithGenerator(t *testing.T) {
	t.Parallel()

	sourceDir := t.TempDir()

	files, err := environment.DeriveMultiClusterLayout("prod")
	require.NoError(t, err)

	_, err = environment.WriteMultiClusterLayout(
		kustomizationgenerator.NewGenerator(), sourceDir, files, false,
	)
	require.NoError(t, err)

	// Re-render each model directly (Output empty → returns the YAML) and compare.
	for _, file := range files {
		want, genErr := kustomizationgenerator.NewGenerator().
			Generate(file.Kustomization, yamlgenerator.Options{})
		require.NoError(t, genErr)

		got := readFile(t, filepath.Join(sourceDir, filepath.FromSlash(file.RelPath)))
		assert.Equal(t, want, got, "written %s must match the generator output", file.RelPath)
	}
}

// TestWriteMultiClusterLayout_IdempotentWhenNotForcing asserts an already-present
// file is left untouched when force is false, while a missing sibling is still
// written — the scaffolder's skip-existing behaviour.
func TestWriteMultiClusterLayout_IdempotentWhenNotForcing(t *testing.T) {
	t.Parallel()

	sourceDir := t.TempDir()
	baseRel, overlayRel := layoutRelPaths()
	basePath := filepath.Join(sourceDir, baseRel)
	seedFile(t, basePath, "# hand-edited base\n")

	files, err := environment.DeriveMultiClusterLayout("prod")
	require.NoError(t, err)

	_, err = environment.WriteMultiClusterLayout(
		kustomizationgenerator.NewGenerator(), sourceDir, files, false,
	)
	require.NoError(t, err)

	assert.Equal(t, "# hand-edited base\n", readFile(t, basePath),
		"an existing file must be preserved when force is false")
	assert.Contains(t, readFile(t, filepath.Join(sourceDir, overlayRel)), "../base",
		"a missing overlay must still be written")
}

// TestWriteMultiClusterLayout_ForceOverwrites asserts force replaces an existing
// file with the generated content.
func TestWriteMultiClusterLayout_ForceOverwrites(t *testing.T) {
	t.Parallel()

	sourceDir := t.TempDir()
	baseRel, _ := layoutRelPaths()
	basePath := filepath.Join(sourceDir, baseRel)
	seedFile(t, basePath, "# stale\n")

	files, err := environment.DeriveMultiClusterLayout("prod")
	require.NoError(t, err)

	_, err = environment.WriteMultiClusterLayout(
		kustomizationgenerator.NewGenerator(), sourceDir, files, true,
	)
	require.NoError(t, err)

	assert.Contains(t, readFile(t, basePath), "kind: Kustomization",
		"force must overwrite the existing file with the generated content")
}

// errGenerate is a sentinel the mock generator returns to assert error wrapping.
var errGenerate = errors.New("boom")

// TestWriteMultiClusterLayout_WrapsGeneratorError asserts a render failure is
// wrapped with the failing file's relative path and aborts with no partial result.
func TestWriteMultiClusterLayout_WrapsGeneratorError(t *testing.T) {
	t.Parallel()

	gen := generator.NewMockGenerator[*ktypes.Kustomization, yamlgenerator.Options](t)
	gen.EXPECT().Generate(mock.Anything, mock.Anything).Return("", errGenerate).Once()

	baseRel, _ := layoutRelPaths()
	files := []environment.LayoutFile{
		{RelPath: baseRel, Kustomization: &ktypes.Kustomization{}},
	}

	written, err := environment.WriteMultiClusterLayout(gen, t.TempDir(), files, false)
	require.ErrorIs(t, err, errGenerate)
	assert.Contains(t, err.Error(), baseRel)
	assert.Nil(t, written)
}

// readFile reads path and fails the test on error.
func readFile(t *testing.T, path string) string {
	t.Helper()

	content, err := os.ReadFile(path) //nolint:gosec // path is a test-owned temp dir.
	require.NoError(t, err)

	return string(content)
}

// seedFile writes content at path, creating parent directories, and fails the test
// on error.
func seedFile(t *testing.T, path, content string) {
	t.Helper()

	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}
