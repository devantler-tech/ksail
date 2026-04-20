package oci_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/client/oci"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEmptyBuildOptionsValidate tests the Validate method on EmptyBuildOptions.
//
//nolint:funlen // Table-driven validation cases are easier to keep together.
func TestEmptyBuildOptionsValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		opts    oci.EmptyBuildOptions
		wantErr error
	}{
		{
			name: "missing registry endpoint",
			opts: oci.EmptyBuildOptions{
				Version:    "v1.0.0",
				Repository: "test-repo",
			},
			wantErr: oci.ErrRegistryEndpointRequired,
		},
		{
			name: "missing repository",
			opts: oci.EmptyBuildOptions{
				RegistryEndpoint: "localhost:5000",
				Version:          "v1.0.0",
			},
			wantErr: oci.ErrRepositoryRequired,
		},
		{
			name: "missing version",
			opts: oci.EmptyBuildOptions{
				RegistryEndpoint: "localhost:5000",
				Repository:       "test-repo",
			},
			wantErr: oci.ErrVersionRequired,
		},
		{
			name: "valid with defaults applied",
			opts: oci.EmptyBuildOptions{
				RegistryEndpoint: "localhost:5000",
				Version:          "v1.0.0",
				Repository:       "my-repo",
			},
			wantErr: nil,
		},
		{
			name: "valid with name and repository",
			opts: oci.EmptyBuildOptions{
				Name:             "my-artifact",
				RegistryEndpoint: "ghcr.io",
				Repository:       "org/project",
				Version:          "latest",
			},
			wantErr: nil,
		},
	}

	for _, tt := range tests { //nolint:varnamelen
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			validated, err := tt.opts.Validate()

			if tt.wantErr != nil {
				require.Error(t, err)
				assert.ErrorIs(t, err, tt.wantErr)

				return
			}

			require.NoError(t, err)
			assert.NotEmpty(t, validated.RegistryEndpoint)
			assert.NotEmpty(t, validated.Version)
		})
	}
}

// TestBuildOptionsValidate_RegistryEndpointNormalization verifies that protocol
// prefixes are stripped from registry endpoints.
//
//nolint:varnamelen // Short names keep table-driven tests readable.
func TestBuildOptionsValidate_RegistryEndpointNormalization(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	testFile := filepath.Join(dir, "test.yaml")
	require.NoError(t, os.WriteFile(testFile, []byte("apiVersion: v1"), 0o600))

	tests := []struct {
		name         string
		endpoint     string
		wantEndpoint string
	}{
		{
			name:         "strips http prefix",
			endpoint:     "http://localhost:5000",
			wantEndpoint: "localhost:5000",
		},
		{
			name:         "strips https prefix",
			endpoint:     "https://ghcr.io",
			wantEndpoint: "ghcr.io",
		},
		{
			name:         "strips oci prefix",
			endpoint:     "oci://registry.example.com:8080",
			wantEndpoint: "registry.example.com:8080",
		},
		{
			name:         "no prefix unchanged",
			endpoint:     "localhost:5000",
			wantEndpoint: "localhost:5000",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			opts := oci.BuildOptions{
				SourcePath:       dir,
				RegistryEndpoint: tt.endpoint,
				Version:          "v1.0.0",
				Repository:       "test-repo",
			}

			validated, err := opts.Validate()
			require.NoError(t, err)
			assert.Equal(t, tt.wantEndpoint, validated.RegistryEndpoint)
		})
	}
}

// TestBuildOptionsValidate_RepositoryNameNormalization verifies repository name
// normalization to lowercase and DNS-safe values.
func TestBuildOptionsValidate_RepositoryNameNormalization(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	testFile := filepath.Join(dir, "test.yaml")
	require.NoError(t, os.WriteFile(testFile, []byte("apiVersion: v1"), 0o600))

	tests := []struct {
		name      string
		repoName  string
		wantLower bool
	}{
		{
			name:      "uppercase letters lowercased",
			repoName:  "MyProject",
			wantLower: true,
		},
		{
			name:      "already lowercase",
			repoName:  "myproject",
			wantLower: true,
		},
		{
			name:      "mixed case",
			repoName:  "My-Project-App",
			wantLower: true,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			opts := oci.BuildOptions{
				SourcePath:       dir,
				RegistryEndpoint: "localhost:5000",
				Version:          "v1.0.0",
				Repository:       testCase.repoName,
			}

			validated, err := opts.Validate()
			require.NoError(t, err)

			expectedRepository := testCase.repoName
			if testCase.wantLower {
				expectedRepository = strings.ToLower(testCase.repoName)
			}

			assert.Equal(t, expectedRepository, validated.Repository)
		})
	}
}

// TestBuildOptionsValidate_GitOpsEnginePreserved verifies that GitOpsEngine
// field values are preserved through validation.
func TestBuildOptionsValidate_GitOpsEnginePreserved(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	testFile := filepath.Join(dir, "test.yaml")
	require.NoError(t, os.WriteFile(testFile, []byte("apiVersion: v1"), 0o600))

	engines := []v1alpha1.GitOpsEngine{
		v1alpha1.GitOpsEngineFlux,
		v1alpha1.GitOpsEngineArgoCD,
		v1alpha1.GitOpsEngineNone,
	}

	for _, engine := range engines {
		t.Run(string(engine), func(t *testing.T) {
			t.Parallel()

			opts := oci.BuildOptions{
				SourcePath:       dir,
				RegistryEndpoint: "localhost:5000",
				Version:          "v1.0.0",
				Repository:       "test",
				GitOpsEngine:     engine,
			}

			validated, err := opts.Validate()
			require.NoError(t, err)
			assert.Equal(t, engine, validated.GitOpsEngine)
		})
	}
}

// TestBuildOptionsValidate_CredentialsPreserved verifies that username and
// password are preserved through validation.
func TestBuildOptionsValidate_CredentialsPreserved(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	testFile := filepath.Join(dir, "test.yaml")
	require.NoError(t, os.WriteFile(testFile, []byte("apiVersion: v1"), 0o600))

	opts := oci.BuildOptions{
		SourcePath:       dir,
		RegistryEndpoint: "ghcr.io",
		Version:          "v1.0.0",
		Repository:       "test",
		Username:         "myuser",
		Password:         "mytoken",
	}

	validated, err := opts.Validate()
	require.NoError(t, err)
	assert.Equal(t, "myuser", validated.Username)
	assert.Equal(t, "mytoken", validated.Password)
}

// TestBuildOptionsValidate_SourcePathNotFound verifies error when source
// path doesn't exist.
func TestBuildOptionsValidate_SourcePathNotFound(t *testing.T) {
	t.Parallel()

	opts := oci.BuildOptions{
		SourcePath:       "/nonexistent/path/that/should/not/exist",
		RegistryEndpoint: "localhost:5000",
		Version:          "v1.0.0",
	}

	_, err := opts.Validate()

	require.Error(t, err)
	assert.ErrorIs(t, err, oci.ErrSourcePathNotFound)
}

// TestBuildOptionsValidate_SourcePathIsFile verifies error when source
// path is a file instead of directory.
func TestBuildOptionsValidate_SourcePathIsFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	filePath := filepath.Join(dir, "notadir.yaml")
	require.NoError(t, os.WriteFile(filePath, []byte("test"), 0o600))

	opts := oci.BuildOptions{
		SourcePath:       filePath,
		RegistryEndpoint: "localhost:5000",
		Version:          "v1.0.0",
	}

	_, err := opts.Validate()

	require.Error(t, err)
	assert.ErrorIs(t, err, oci.ErrSourcePathNotDirectory)
}

// TestBuildOptionsValidate_DefaultRepoFromSourceDir verifies that when repository
// is not specified, it's derived from the source directory name.
func TestBuildOptionsValidate_DefaultRepoFromSourceDir(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	sourceDir := filepath.Join(dir, "my-manifests")
	require.NoError(t, os.MkdirAll(sourceDir, 0o750))

	testFile := filepath.Join(sourceDir, "test.yaml")
	require.NoError(t, os.WriteFile(testFile, []byte("apiVersion: v1"), 0o600))

	opts := oci.BuildOptions{
		SourcePath:       sourceDir,
		RegistryEndpoint: "localhost:5000",
		Version:          "v1.0.0",
		// Repository not set — should default from source dir name
	}

	validated, err := opts.Validate()
	require.NoError(t, err)
	assert.NotEmpty(t, validated.Repository, "repository should be derived from source dir")
}

// TestEmptyBuildOptionsValidate_EndpointNormalization verifies endpoint
// normalization for empty build options.
//
//nolint:varnamelen // Short names keep table-driven tests readable.
func TestEmptyBuildOptionsValidate_EndpointNormalization(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		endpoint     string
		wantEndpoint string
	}{
		{
			name:         "strips http prefix",
			endpoint:     "http://localhost:5000",
			wantEndpoint: "localhost:5000",
		},
		{
			name:         "strips oci prefix",
			endpoint:     "oci://registry.example.com",
			wantEndpoint: "registry.example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			opts := oci.EmptyBuildOptions{
				RegistryEndpoint: tt.endpoint,
				Version:          "v1.0.0",
				Repository:       "test-repo",
			}

			validated, err := opts.Validate()
			require.NoError(t, err)
			assert.Equal(t, tt.wantEndpoint, validated.RegistryEndpoint)
		})
	}
}
