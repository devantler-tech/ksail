package flux_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	snapshottest "github.com/devantler-tech/ksail/v7/internal/testutil/snapshottest"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil/generator/flux"
	yamlgenerator "github.com/devantler-tech/ksail/v7/pkg/fsutil/generator/yaml"
	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/stretchr/testify/require"
)

const testFilePermissions = 0o600

func TestMain(m *testing.M) {
	os.Exit(snapshottest.Run(m, snaps.CleanOpts{Sort: true}))
}

func TestInstanceGenerator_Generate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		opts flux.InstanceGeneratorOptions
	}{
		{
			name: "with default values",
			opts: flux.InstanceGeneratorOptions{},
		},
		{
			name: "with custom registry host and port",
			opts: flux.InstanceGeneratorOptions{
				RegistryHost: "custom-registry.localhost",
				RegistryPort: 8080,
				ProjectName:  "my-project",
			},
		},
		{
			name: "with custom interval",
			opts: flux.InstanceGeneratorOptions{
				RegistryHost: "registry.localhost",
				RegistryPort: 5000,
				ProjectName:  "test-project",
				Interval:     5 * time.Minute,
			},
		},
		{
			name: "with zero port uses default",
			opts: flux.InstanceGeneratorOptions{
				RegistryHost: "registry.localhost",
				RegistryPort: 0,
				ProjectName:  "test-project",
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			gen := flux.NewInstanceGenerator()
			result, err := gen.Generate(testCase.opts)

			require.NoError(t, err)
			require.NotEmpty(t, result)
			snaps.MatchSnapshot(t, result)
		})
	}
}

func TestInstanceGenerator_GenerateToFile(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	outputPath := filepath.Join(tempDir, "flux-instance.yaml")

	gen := flux.NewInstanceGenerator()
	opts := flux.InstanceGeneratorOptions{
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

func TestInstanceGenerator_GenerateWithForce(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	outputPath := filepath.Join(tempDir, "flux-instance.yaml")

	// Write existing content
	err := os.WriteFile(outputPath, []byte("existing content"), testFilePermissions)
	require.NoError(t, err)

	gen := flux.NewInstanceGenerator()
	opts := flux.InstanceGeneratorOptions{
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

func TestDefaultInterval(t *testing.T) {
	t.Parallel()

	require.Equal(t, time.Minute, flux.DefaultInterval)
}
