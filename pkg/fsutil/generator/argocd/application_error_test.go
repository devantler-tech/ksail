package argocd_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/fsutil/generator/argocd"
	yamlgenerator "github.com/devantler-tech/ksail/v7/pkg/fsutil/generator/yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGenerate_WriteError verifies that a write error is propagated.
func TestGenerate_WriteError(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	blockingPath := filepath.Join(tmpDir, "blocking-file")
	require.NoError(t, os.WriteFile(blockingPath, []byte("blocker"), 0o600))

	generator := argocd.NewApplicationGenerator()

	opts := argocd.ApplicationGeneratorOptions{
		Options: yamlgenerator.Options{
			Output: filepath.Join(blockingPath, "subdir", "application.yaml"),
			Force:  true,
		},
		ProjectName:  "test-project",
		RegistryHost: "localhost",
		RegistryPort: 5050,
	}

	_, err := generator.Generate(opts)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "generating ArgoCD Application manifest")
}
