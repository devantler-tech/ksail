package registry_test

import (
	"os"
	"path/filepath"
	"testing"

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

func TestReadExistingHostsToml(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		setupFunc     func(t *testing.T) string
		expectedSpecs []registry.MirrorSpec
		expectedError bool
	}{
		{
			name:      "reads existing hosts.toml files",
			setupFunc: setupMultipleHosts,
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
			name:      "skips directories without hosts.toml",
			setupFunc: setupWithEmptyDir,
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
	expectedSpecs []registry.MirrorSpec,
	expectedError bool,
	specs []registry.MirrorSpec,
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
