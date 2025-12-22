package registry_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateHostsToml(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		entry    registry.MirrorEntry
		expected string
	}{
		{
			name: "docker.io with custom remote",
			entry: registry.MirrorEntry{
				Host:     "docker.io",
				Endpoint: "http://docker.io:5000",
				Remote:   "https://registry-1.docker.io",
			},
			expected: `server = "https://registry-1.docker.io"

[host."http://docker.io:5000"]
  capabilities = ["pull", "resolve"]
`,
		},
		{
			name: "ghcr.io without custom remote",
			entry: registry.MirrorEntry{
				Host:     "ghcr.io",
				Endpoint: "http://ghcr.io:5001",
				Remote:   "",
			},
			expected: `server = "https://ghcr.io"

[host."http://ghcr.io:5001"]
  capabilities = ["pull", "resolve"]
`,
		},
		{
			name: "custom registry with port",
			entry: registry.MirrorEntry{
				Host:     "registry.example.com:443",
				Endpoint: "http://registry.example.com-443:5002",
				Remote:   "https://registry.example.com:443",
			},
			expected: `server = "https://registry.example.com:443"

[host."http://registry.example.com-443:5002"]
  capabilities = ["pull", "resolve"]
`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result := registry.GenerateHostsToml(tc.entry)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestHostsDirectoryManager_WriteHostsToml(t *testing.T) {
	t.Parallel()

	// Create temporary directory for test
	tempDir := t.TempDir()

	mgr, err := registry.NewHostsDirectoryManager(tempDir)
	require.NoError(t, err)
	require.NotNil(t, mgr)

	entry := registry.MirrorEntry{
		Host:     "docker.io",
		Endpoint: "http://docker.io:5000",
		Remote:   "https://registry-1.docker.io",
	}

	dir, err := mgr.WriteHostsToml(entry)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(tempDir, "docker.io"), dir)

	// Verify hosts.toml file was created
	hostsPath := filepath.Join(dir, "hosts.toml")
	assert.FileExists(t, hostsPath)

	// Verify content
	content, err := os.ReadFile(hostsPath)
	require.NoError(t, err)

	expected := `server = "https://registry-1.docker.io"

[host."http://docker.io:5000"]
  capabilities = ["pull", "resolve"]
`
	assert.Equal(t, expected, string(content))
}

func TestHostsDirectoryManager_WriteAllHostsToml(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	mgr, err := registry.NewHostsDirectoryManager(tempDir)
	require.NoError(t, err)

	entries := []registry.MirrorEntry{
		{
			Host:     "docker.io",
			Endpoint: "http://docker.io:5000",
			Remote:   "https://registry-1.docker.io",
		},
		{
			Host:     "ghcr.io",
			Endpoint: "http://ghcr.io:5001",
			Remote:   "https://ghcr.io",
		},
	}

	result, err := mgr.WriteAllHostsToml(entries)
	require.NoError(t, err)
	assert.Len(t, result, 2)

	// Verify docker.io
	dockerDir := result["docker.io"]
	assert.Equal(t, filepath.Join(tempDir, "docker.io"), dockerDir)
	assert.FileExists(t, filepath.Join(dockerDir, "hosts.toml"))

	// Verify ghcr.io
	ghcrDir := result["ghcr.io"]
	assert.Equal(t, filepath.Join(tempDir, "ghcr.io"), ghcrDir)
	assert.FileExists(t, filepath.Join(ghcrDir, "hosts.toml"))
}

func TestHostsDirectoryManager_EmptyBaseDir(t *testing.T) {
	t.Parallel()

	mgr, err := registry.NewHostsDirectoryManager("")
	assert.Error(t, err)
	assert.Nil(t, mgr)
	assert.ErrorContains(t, err, "baseDir cannot be empty")
}

func TestHostsDirectoryManager_Cleanup(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	hostsDir := filepath.Join(tempDir, "hosts")

	mgr, err := registry.NewHostsDirectoryManager(hostsDir)
	require.NoError(t, err)

	entry := registry.MirrorEntry{
		Host:     "docker.io",
		Endpoint: "http://docker.io:5000",
		Remote:   "https://registry-1.docker.io",
	}

	_, err = mgr.WriteHostsToml(entry)
	require.NoError(t, err)

	// Verify directory exists
	assert.DirExists(t, hostsDir)

	// Cleanup
	err = mgr.Cleanup()
	require.NoError(t, err)

	// Verify directory was removed
	assert.NoDirExists(t, hostsDir)
}
