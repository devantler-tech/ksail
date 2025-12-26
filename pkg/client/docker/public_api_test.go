package docker_test

import (
	"testing"

	docker "github.com/devantler-tech/ksail/v5/pkg/client/docker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRegistryManager_Success(t *testing.T) {
	t.Parallel()

	mockClient := docker.NewMockAPIClient(t)

	manager, err := docker.NewRegistryManager(mockClient)

	require.NoError(t, err)
	assert.NotNil(t, manager)
}

func TestNewRegistryManager_NilClient(t *testing.T) {
	t.Parallel()

	manager, err := docker.NewRegistryManager(nil)

	require.Error(t, err)
	assert.Nil(t, manager)
	assert.ErrorIs(t, err, docker.ErrAPIClientNil)
}

func TestRegistryConfig_DefaultValues(t *testing.T) {
	t.Parallel()

	config := docker.RegistryConfig{
		Name: "docker.io",
		Port: 5000,
	}

	assert.Equal(t, "docker.io", config.Name)
	assert.Equal(t, 5000, config.Port)
	assert.Empty(t, config.UpstreamURL)
	assert.Empty(t, config.ClusterName)
	assert.Empty(t, config.NetworkName)
	assert.Empty(t, config.VolumeName)
}

func TestRegistryConfig_WithAllFields(t *testing.T) {
	t.Parallel()

	config := docker.RegistryConfig{
		Name:        "ghcr.io",
		Port:        5001,
		UpstreamURL: "https://ghcr.io",
		ClusterName: "my-cluster",
		NetworkName: "my-network",
		VolumeName:  "my-volume",
	}

	assert.Equal(t, "ghcr.io", config.Name)
	assert.Equal(t, 5001, config.Port)
	assert.Equal(t, "https://ghcr.io", config.UpstreamURL)
	assert.Equal(t, "my-cluster", config.ClusterName)
	assert.Equal(t, "my-network", config.NetworkName)
	assert.Equal(t, "my-volume", config.VolumeName)
}

func TestNormalizeVolumeName_SimpleNames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple name",
			input:    "docker.io",
			expected: "docker.io",
		},
		{
			name:     "with port",
			input:    "localhost:5000",
			expected: "localhost:5000",
		},
		{
			name:     "kind prefix stripped",
			input:    "kind-registry",
			expected: "registry",
		},
		{
			name:     "k3d prefix stripped",
			input:    "k3d-registry",
			expected: "registry",
		},
		{
			name:     "kind prefix with dots",
			input:    "kind-docker.io",
			expected: "docker.io",
		},
		{
			name:     "k3d prefix with colons",
			input:    "k3d-localhost:5000",
			expected: "localhost:5000",
		},
		{
			name:     "no prefix",
			input:    "registry-name",
			expected: "registry-name",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "whitespace trimmed",
			input:    "  docker.io  ",
			expected: "docker.io",
		},
	}

	for i := range tests {
		testCase := tests[i]

		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := docker.NormalizeVolumeName(testCase.input)

			assert.Equal(t, testCase.expected, result)
		})
	}
}

func TestConstants_RegistryDefaults(t *testing.T) {
	t.Parallel()

	// Test that registry constants are defined with expected values
	assert.Equal(t, "registry:3", docker.RegistryImageName)
	assert.Equal(t, "io.ksail.registry", docker.RegistryLabelKey)
	assert.Equal(t, 5000, docker.DefaultRegistryPort)
	assert.Equal(t, 5000, docker.RegistryPortBase)
	assert.Equal(t, 2, docker.HostPortParts)
	assert.Equal(t, "5000/tcp", docker.RegistryContainerPort)
	assert.Equal(t, "127.0.0.1", docker.RegistryHostIP)
	assert.Equal(t, "/var/lib/registry", docker.RegistryDataPath)
	assert.Equal(t, "unless-stopped", docker.RegistryRestartPolicy)
}

func TestErrConstants(t *testing.T) {
	t.Parallel()

	// Test that error constants are defined
	assert.NotNil(t, docker.ErrAPIClientNil)
	assert.NotNil(t, docker.ErrRegistryNotFound)
	assert.NotNil(t, docker.ErrRegistryAlreadyExists)
	assert.NotNil(t, docker.ErrRegistryPortNotFound)

	// Test error messages contain useful information
	assert.Contains(t, docker.ErrAPIClientNil.Error(), "apiClient")
	assert.Contains(t, docker.ErrRegistryNotFound.Error(), "registry")
	assert.Contains(t, docker.ErrRegistryAlreadyExists.Error(), "already exists")
	assert.Contains(t, docker.ErrRegistryPortNotFound.Error(), "port")
}
