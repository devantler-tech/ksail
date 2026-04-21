package argocd_test

import (
	"os"
	"path/filepath"
	"testing"

	snapshottest "github.com/devantler-tech/ksail/v7/internal/testutil/snapshottest"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil/generator/argocd"
	yamlgenerator "github.com/devantler-tech/ksail/v7/pkg/fsutil/generator/yaml"
	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/stretchr/testify/require"
)

const testFilePermissions = 0o600

func TestMain(m *testing.M) {
	os.Exit(snapshottest.Run(m, snaps.CleanOpts{Sort: true}))
}

func TestApplicationGenerator_Generate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		opts argocd.ApplicationGeneratorOptions
	}{
		{
			name: "with default values",
			opts: argocd.ApplicationGeneratorOptions{},
		},
		{
			name: "with custom registry host and port",
			opts: argocd.ApplicationGeneratorOptions{
				RegistryHost: "custom-registry.localhost",
				RegistryPort: 8080,
				ProjectName:  "my-project",
			},
		},
		{
			name: "with zero port uses default",
			opts: argocd.ApplicationGeneratorOptions{
				RegistryHost: "registry.localhost",
				RegistryPort: 0,
				ProjectName:  "test-project",
			},
		},
		{
			name: "with empty project name uses default",
			opts: argocd.ApplicationGeneratorOptions{
				RegistryHost: "registry.localhost",
				RegistryPort: 5000,
				ProjectName:  "",
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			gen := argocd.NewApplicationGenerator()
			result, err := gen.Generate(testCase.opts)

			require.NoError(t, err)
			require.NotEmpty(t, result)
			snaps.MatchSnapshot(t, result)
		})
	}
}

func TestApplicationGenerator_GenerateToFile(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	outputPath := filepath.Join(tempDir, "application.yaml")

	gen := argocd.NewApplicationGenerator()
	opts := argocd.ApplicationGeneratorOptions{
		Options: yamlgenerator.Options{
			Output: outputPath,
		},
		RegistryHost: "registry.localhost",
		RegistryPort: 5000,
		ProjectName:  "test-project",
	}

	result, err := gen.Generate(opts)
	require.NoError(t, err)
	require.NotEmpty(t, result)

	// Verify file was written
	content, err := os.ReadFile(outputPath) //nolint:gosec // Test file path is safe
	require.NoError(t, err)
	require.Equal(t, result, string(content))
}

func TestApplicationGenerator_GenerateWithForce(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	outputPath := filepath.Join(tempDir, "application.yaml")

	// Write existing content
	err := os.WriteFile(outputPath, []byte("existing content"), testFilePermissions)
	require.NoError(t, err)

	gen := argocd.NewApplicationGenerator()
	opts := argocd.ApplicationGeneratorOptions{
		Options: yamlgenerator.Options{
			Output: outputPath,
			Force:  true,
		},
		RegistryHost: "registry.localhost",
		RegistryPort: 5000,
		ProjectName:  "test-project",
	}

	result, err := gen.Generate(opts)
	require.NoError(t, err)
	require.NotEmpty(t, result)

	// Verify file was overwritten
	content, err := os.ReadFile(outputPath) //nolint:gosec // Test file path is safe
	require.NoError(t, err)
	require.Equal(t, result, string(content))
	require.NotEqual(t, "existing content", string(content))
}
