package argocd_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v6/pkg/fsutil/generator/argocd"
	yamlgenerator "github.com/devantler-tech/ksail/v6/pkg/fsutil/generator/yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGenerate_WriteError verifies that a write error is propagated when
// the output directory is not writable.
func TestGenerate_WriteError(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	readOnlyDir := filepath.Join(tmpDir, "readonly")

	err := os.MkdirAll(readOnlyDir, 0o500)
	require.NoError(t, err)

	t.Cleanup(func() { _ = os.Chmod(readOnlyDir, 0o700) })

	g := argocd.NewApplicationGenerator()

	opts := argocd.ApplicationGeneratorOptions{
		Options: yamlgenerator.Options{
			Output: filepath.Join(readOnlyDir, "subdir", "application.yaml"),
			Force:  true,
		},
		ProjectName:  "test-project",
		RegistryHost: "localhost",
		RegistryPort: 5050,
	}

	_, err = g.Generate(opts)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "generating ArgoCD Application manifest")
}
