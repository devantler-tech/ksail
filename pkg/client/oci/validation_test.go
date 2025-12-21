package oci_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	v1alpha1 "github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/client/oci"
	"github.com/stretchr/testify/require"
)

// setupValidationTest creates a temporary source directory (if createDir is true) and validates BuildOptions.
func setupValidationTest(
	t *testing.T,
	source string,
	registry string,
	version string,
	createDir bool,
) (oci.ValidatedBuildOptions, error) {
	t.Helper()

	if createDir {
		require.NoError(t, os.MkdirAll(source, 0o750))
	}

	opts := oci.BuildOptions{SourcePath: source, RegistryEndpoint: registry, Version: version}

	validated, err := opts.Validate()
	if err != nil {
		return oci.ValidatedBuildOptions{}, fmt.Errorf("validate build options: %w", err)
	}

	return validated, nil
}

//nolint:funlen // Table-driven test with many scenarios
func TestBuildOptionsValidate(t *testing.T) {
	t.Parallel()

	t.Run("requires source path", func(t *testing.T) {
		t.Parallel()

		opts := oci.BuildOptions{}

		_, err := opts.Validate()

		require.ErrorIs(t, err, oci.ErrSourcePathRequired)
	})

	t.Run("fails when source path missing", func(t *testing.T) {
		t.Parallel()

		opts := oci.BuildOptions{
			SourcePath:       filepath.Join(t.TempDir(), "missing"),
			RegistryEndpoint: "localhost:5000",
			Version:          "1.0.0",
		}

		_, err := opts.Validate()

		require.ErrorIs(t, err, oci.ErrSourcePathNotFound)
	})

	t.Run("fails when source path is file", func(t *testing.T) {
		t.Parallel()

		file := filepath.Join(t.TempDir(), "manifest.yaml")
		require.NoError(t, os.WriteFile(file, []byte("apiVersion: v1"), 0o600))

		opts := oci.BuildOptions{
			SourcePath:       file,
			RegistryEndpoint: "localhost:5000",
			Version:          "1.0.0",
		}

		_, err := opts.Validate()

		require.ErrorIs(t, err, oci.ErrSourcePathNotDirectory)
	})

	t.Run("requires registry endpoint", func(t *testing.T) {
		t.Parallel()

		tempDir := t.TempDir()

		opts := oci.BuildOptions{SourcePath: tempDir, Version: "1.0.0"}

		_, err := opts.Validate()

		require.ErrorIs(t, err, oci.ErrRegistryEndpointRequired)
	})

	t.Run("requires version", func(t *testing.T) {
		t.Parallel()

		tempDir := t.TempDir()

		opts := oci.BuildOptions{SourcePath: tempDir, RegistryEndpoint: "localhost:5000"}

		_, err := opts.Validate()

		require.ErrorIs(t, err, oci.ErrVersionRequired)
	})

	t.Run("accepts any non-empty version", func(t *testing.T) {
		t.Parallel()

		tempDir := t.TempDir()

		opts := oci.BuildOptions{
			SourcePath:       tempDir,
			RegistryEndpoint: "localhost:5000",
			Version:          "dev",
		}

		validated, err := opts.Validate()

		require.NoError(t, err)
		require.Equal(t, "dev", validated.Version)
	})

	t.Run("allows latest tag", func(t *testing.T) {
		t.Parallel()

		source := filepath.Join(t.TempDir(), "k8s")

		validated, err := setupValidationTest(t, source, "localhost:5000", "latest", true)

		require.NoError(t, err)
		require.Equal(t, "latest", validated.Version)
	})

	t.Run("applies defaults", func(t *testing.T) {
		t.Parallel()

		source := filepath.Join(t.TempDir(), "k8s")

		validated, err := setupValidationTest(t, source, "localhost:5000", "1.0.0", true)

		require.NoError(t, err)
		require.Equal(t, filepath.Clean(source), validated.SourcePath)
		require.Equal(t, "localhost:5000", validated.RegistryEndpoint)
		require.Equal(t, "1.0.0", validated.Version)
		require.NotEmpty(t, validated.Repository)
	})

	t.Run("normalizes repository name", func(t *testing.T) {
		t.Parallel()

		source := filepath.Join(t.TempDir(), "my App")
		require.NoError(t, os.MkdirAll(source, 0o750))

		opts := oci.BuildOptions{
			SourcePath:       source,
			RegistryEndpoint: "localhost:5000",
			Version:          "1.0.0",
			Repository:       "  KSail/Workloads/My-App  ",
		}

		validated, err := opts.Validate()

		require.NoError(t, err)
		require.Equal(t, "ksail/workloads/my-app", validated.Repository)
	})

	t.Run("preserves GitOpsEngine field", func(t *testing.T) {
		t.Parallel()

		// Test with various GitOpsEngine values
		testCases := []struct {
			name          string
			gitOpsEngine  v1alpha1.GitOpsEngine
			expectedValue v1alpha1.GitOpsEngine
		}{
			{
				name:          "Flux engine",
				gitOpsEngine:  v1alpha1.GitOpsEngineFlux,
				expectedValue: v1alpha1.GitOpsEngineFlux,
			},
			{
				name:          "ArgoCD engine",
				gitOpsEngine:  v1alpha1.GitOpsEngineArgoCD,
				expectedValue: v1alpha1.GitOpsEngineArgoCD,
			},
			{
				name:          "None engine",
				gitOpsEngine:  v1alpha1.GitOpsEngineNone,
				expectedValue: v1alpha1.GitOpsEngineNone,
			},
			{
				name:          "empty engine",
				gitOpsEngine:  "",
				expectedValue: "",
			},
		}

		for _, testCase := range testCases {
			t.Run(testCase.name, func(t *testing.T) {
				t.Parallel()

				source := filepath.Join(t.TempDir(), "k8s")
				require.NoError(t, os.MkdirAll(source, 0o750))

				opts := oci.BuildOptions{
					SourcePath:       source,
					RegistryEndpoint: "localhost:5000",
					Version:          "1.0.0",
					GitOpsEngine:     testCase.gitOpsEngine,
				}

				validated, err := opts.Validate()

				require.NoError(t, err)
				require.Equal(t, testCase.expectedValue, validated.GitOpsEngine)
			})
		}
	})
}
