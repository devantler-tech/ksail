package helm_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// validateRepositoryRequest
// ---------------------------------------------------------------------------

//nolint:containedctx // Test table keeps context fixtures explicit.
func TestValidateRepositoryRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		entry   *helm.RepositoryEntry
		ctx     context.Context
		wantErr error
	}{
		{
			name:    "nil entry",
			entry:   nil,
			ctx:     context.Background(),
			wantErr: helm.ErrRepositoryEntryRequired,
		},
		{
			name:    "empty name",
			entry:   &helm.RepositoryEntry{Name: "", URL: "https://example.com"},
			ctx:     context.Background(),
			wantErr: helm.ErrRepositoryNameRequired,
		},
		{
			name:  "cancelled context",
			entry: &helm.RepositoryEntry{Name: "repo", URL: "https://example.com"},
			ctx: func() context.Context {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()

				return ctx
			}(),
			wantErr: context.Canceled,
		},
		{
			name:  "valid request",
			entry: &helm.RepositoryEntry{Name: "repo", URL: "https://example.com"},
			ctx:   context.Background(),
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := helm.ValidateRepositoryRequest(testCase.ctx, testCase.entry)

			if testCase.wantErr != nil {
				require.Error(t, err)
				assert.ErrorIs(t, err, testCase.wantErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// convertRepositoryEntry
// ---------------------------------------------------------------------------

func TestConvertRepositoryEntry(t *testing.T) {
	t.Parallel()

	t.Run("full entry is converted correctly", func(t *testing.T) {
		t.Parallel()

		entry := &helm.RepositoryEntry{
			Name:                  "my-repo",
			URL:                   "https://charts.example.com",
			Username:              "user",
			Password:              "pass",
			CertFile:              "/cert",
			KeyFile:               "/key",
			CaFile:                "/ca",
			InsecureSkipTLSverify: true,
		}

		result := helm.ConvertRepositoryEntry(entry)

		assert.Equal(t, "my-repo", result.Name)
		assert.Equal(t, "https://charts.example.com", result.URL)
		assert.Equal(t, "user", result.Username)
		assert.Equal(t, "pass", result.Password)
		assert.Equal(t, "/cert", result.CertFile)
		assert.Equal(t, "/key", result.KeyFile)
		assert.Equal(t, "/ca", result.CAFile)
		assert.True(t, result.InsecureSkipTLSVerify)
	})

	t.Run("minimal entry", func(t *testing.T) {
		t.Parallel()

		entry := &helm.RepositoryEntry{
			Name: "simple",
			URL:  "https://example.com",
		}

		result := helm.ConvertRepositoryEntry(entry)

		assert.Equal(t, "simple", result.Name)
		assert.Empty(t, result.Username)
		assert.False(t, result.InsecureSkipTLSVerify)
	})
}

// ---------------------------------------------------------------------------
// loadOrInitRepositoryFile
// ---------------------------------------------------------------------------

func TestLoadOrInitRepositoryFile(t *testing.T) {
	t.Parallel()

	t.Run("nonexistent file returns new file", func(t *testing.T) {
		t.Parallel()

		result := helm.LoadOrInitRepositoryFile("/nonexistent/path/repos.yaml")

		require.NotNil(t, result)
	})

	t.Run("valid file loads correctly", func(t *testing.T) {
		t.Parallel()

		tmpDir := t.TempDir()
		repoFile := filepath.Join(tmpDir, "repositories.yaml")

		content := `apiVersion: ""
generated: "0001-01-01T00:00:00Z"
repositories: []
`
		err := os.WriteFile(repoFile, []byte(content), 0o600)
		require.NoError(t, err)

		result := helm.LoadOrInitRepositoryFile(repoFile)

		require.NotNil(t, result)
	})
}

// ---------------------------------------------------------------------------
// RepoConfig and ChartConfig structs
// ---------------------------------------------------------------------------

func TestRepoConfig(t *testing.T) {
	t.Parallel()

	config := helm.RepoConfig{
		Name:     "test",
		URL:      "https://example.com",
		RepoName: "Test Repo",
	}

	assert.Equal(t, "test", config.Name)
	assert.Equal(t, "https://example.com", config.URL)
	assert.Equal(t, "Test Repo", config.RepoName)
}

func TestChartConfig(t *testing.T) {
	t.Parallel()

	config := helm.ChartConfig{
		ReleaseName:     "my-release",
		ChartName:       "my-chart",
		Namespace:       "default",
		Version:         "1.0.0",
		RepoURL:         "https://charts.example.com",
		CreateNamespace: true,
		SetJSONVals:     map[string]string{"key": "value"},
		SkipWait:        true,
	}

	assert.Equal(t, "my-release", config.ReleaseName)
	assert.Equal(t, "my-chart", config.ChartName)
	assert.Equal(t, "default", config.Namespace)
	assert.Equal(t, "1.0.0", config.Version)
	assert.Equal(t, "https://charts.example.com", config.RepoURL)
	assert.True(t, config.CreateNamespace)
	assert.Equal(t, map[string]string{"key": "value"}, config.SetJSONVals)
	assert.True(t, config.SkipWait)
}
