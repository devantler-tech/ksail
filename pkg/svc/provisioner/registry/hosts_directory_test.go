package registry_test

import (
	"os"
	"path/filepath"
	"testing"

	registryutil "github.com/devantler-tech/ksail/v5/pkg/registry"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test data constants for hosts.toml content.
const (
	dockerHostsToml = `server = "https://registry-1.docker.io"

[host."http://docker.io:5000"]
  capabilities = ["pull", "resolve"]
`
	ghcrHostsToml = `server = "https://ghcr.io"

[host."http://ghcr.io:5000"]
  capabilities = ["pull", "resolve"]
`
)

func TestGenerateHostsToml(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		entry    registryutil.MirrorEntry
		expected string
	}{
		{
			name: "docker.io with custom remote",
			entry: registryutil.MirrorEntry{
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
			entry: registryutil.MirrorEntry{
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
			entry: registryutil.MirrorEntry{
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

			result := registryutil.GenerateHostsToml(tc.entry)
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

	entry := registryutil.MirrorEntry{
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
	content, err := os.ReadFile(hostsPath) //nolint:gosec // Test file with controlled path
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

	entries := []registryutil.MirrorEntry{
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
	require.Error(t, err)
	assert.Nil(t, mgr)
	assert.ErrorContains(t, err, "baseDir cannot be empty")
}

func TestHostsDirectoryManager_Cleanup(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	hostsDir := filepath.Join(tempDir, "hosts")

	mgr, err := registry.NewHostsDirectoryManager(hostsDir)
	require.NoError(t, err)

	entry := registryutil.MirrorEntry{
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

func TestReadExistingHostsToml(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		setupFunc     func(t *testing.T) string
		expectedSpecs []registryutil.MirrorSpec
		expectedError bool
	}{
		{
			name:      "reads existing hosts.toml files",
			setupFunc: setupMultipleHosts,
			expectedSpecs: []registryutil.MirrorSpec{
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
			name:      "skips directories without hosts.toml",
			setupFunc: setupWithEmptyDir,
			expectedSpecs: []registryutil.MirrorSpec{
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
			assertSpecs(t, tc.expectedSpecs, tc.expectedError, specs, err)
		})
	}
}

func setupMultipleHosts(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()

	dockerDir := filepath.Join(tmpDir, "docker.io")
	require.NoError(t, os.MkdirAll(dockerDir, 0o750))
	require.NoError(
		t,
		os.WriteFile(filepath.Join(dockerDir, "hosts.toml"), []byte(dockerHostsToml), 0o600),
	)

	ghcrDir := filepath.Join(tmpDir, "ghcr.io")
	require.NoError(t, os.MkdirAll(ghcrDir, 0o750))
	require.NoError(
		t,
		os.WriteFile(filepath.Join(ghcrDir, "hosts.toml"), []byte(ghcrHostsToml), 0o600),
	)

	return tmpDir
}

func setupWithEmptyDir(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()

	emptyDir := filepath.Join(tmpDir, "empty")
	require.NoError(t, os.MkdirAll(emptyDir, 0o750))

	dockerDir := filepath.Join(tmpDir, "docker.io")
	require.NoError(t, os.MkdirAll(dockerDir, 0o750))
	require.NoError(
		t,
		os.WriteFile(filepath.Join(dockerDir, "hosts.toml"), []byte(dockerHostsToml), 0o600),
	)

	return tmpDir
}

func assertSpecs(
	t *testing.T,
	expectedSpecs []registryutil.MirrorSpec,
	expectedError bool,
	specs []registryutil.MirrorSpec,
	err error,
) {
	t.Helper()

	if expectedError {
		require.Error(t, err)

		return
	}

	require.NoError(t, err)

	if expectedSpecs == nil {
		assert.Nil(t, specs)

		return
	}

	require.Len(t, specs, len(expectedSpecs))

	specMap := make(map[string]string)
	for _, spec := range specs {
		specMap[spec.Host] = spec.Remote
	}

	for _, expected := range expectedSpecs {
		remote, ok := specMap[expected.Host]
		assert.True(t, ok, "Expected host %s not found", expected.Host)
		assert.Equal(t, expected.Remote, remote, "Remote mismatch for host %s", expected.Host)
	}
}
