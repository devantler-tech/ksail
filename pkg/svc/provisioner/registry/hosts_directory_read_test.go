package registry_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadExistingHostsToml(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		setupFunc      func(t *testing.T) string
		expectedSpecs  []registry.MirrorSpec
		expectedError  bool
	}{
		{
			name: "reads existing hosts.toml files",
			setupFunc: func(t *testing.T) string {
				t.Helper()
				tmpDir := t.TempDir()
				
				// Create docker.io hosts.toml
				dockerDir := filepath.Join(tmpDir, "docker.io")
				require.NoError(t, os.MkdirAll(dockerDir, 0o755))
				dockerContent := `server = "https://registry-1.docker.io"

[host."http://docker.io:5000"]
  capabilities = ["pull", "resolve"]
`
				require.NoError(t, os.WriteFile(filepath.Join(dockerDir, "hosts.toml"), []byte(dockerContent), 0o644))

				// Create ghcr.io hosts.toml
				ghcrDir := filepath.Join(tmpDir, "ghcr.io")
				require.NoError(t, os.MkdirAll(ghcrDir, 0o755))
				ghcrContent := `server = "https://ghcr.io"

[host."http://ghcr.io:5000"]
  capabilities = ["pull", "resolve"]
`
				require.NoError(t, os.WriteFile(filepath.Join(ghcrDir, "hosts.toml"), []byte(ghcrContent), 0o644))

				return tmpDir
			},
			expectedSpecs: []registry.MirrorSpec{
				{Host: "docker.io", Remote: "https://registry-1.docker.io"},
				{Host: "ghcr.io", Remote: "https://ghcr.io"},
			},
			expectedError: false,
		},
		{
			name: "returns empty when directory doesn't exist",
			setupFunc: func(t *testing.T) string {
				t.Helper()
				return filepath.Join(t.TempDir(), "nonexistent")
			},
			expectedSpecs: nil,
			expectedError: false,
		},
		{
			name: "skips directories without hosts.toml",
			setupFunc: func(t *testing.T) string {
				t.Helper()
				tmpDir := t.TempDir()
				
				// Create directory without hosts.toml
				emptyDir := filepath.Join(tmpDir, "empty")
				require.NoError(t, os.MkdirAll(emptyDir, 0o755))

				// Create directory with hosts.toml
				dockerDir := filepath.Join(tmpDir, "docker.io")
				require.NoError(t, os.MkdirAll(dockerDir, 0o755))
				dockerContent := `server = "https://registry-1.docker.io"

[host."http://docker.io:5000"]
  capabilities = ["pull", "resolve"]
`
				require.NoError(t, os.WriteFile(filepath.Join(dockerDir, "hosts.toml"), []byte(dockerContent), 0o644))

				return tmpDir
			},
			expectedSpecs: []registry.MirrorSpec{
				{Host: "docker.io", Remote: "https://registry-1.docker.io"},
			},
			expectedError: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			baseDir := tc.setupFunc(t)

			specs, err := registry.ReadExistingHostsToml(baseDir)

			if tc.expectedError {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			
			// Sort both slices for comparison (since map iteration order is not guaranteed)
			if tc.expectedSpecs == nil {
				assert.Nil(t, specs)
			} else {
				require.Len(t, specs, len(tc.expectedSpecs))
				
				// Create a map for easier comparison
				specMap := make(map[string]string)
				for _, spec := range specs {
					specMap[spec.Host] = spec.Remote
				}
				
				for _, expected := range tc.expectedSpecs {
					remote, ok := specMap[expected.Host]
					assert.True(t, ok, "Expected host %s not found", expected.Host)
					assert.Equal(t, expected.Remote, remote, "Remote mismatch for host %s", expected.Host)
				}
			}
		})
	}
}
