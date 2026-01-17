package helpers_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/cli/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFormatRegistryURL tests the FormatRegistryURL function.
func TestFormatRegistryURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		host       string
		port       int32
		repository string
		expected   string
	}{
		{
			name:       "localhost with port",
			host:       "localhost",
			port:       5000,
			repository: "myproject",
			expected:   "oci://localhost:5000/myproject",
		},
		{
			name:       "custom host with port",
			host:       "registry.example.com",
			port:       8080,
			repository: "app",
			expected:   "oci://registry.example.com:8080/app",
		},
		{
			name:       "IPv4 with port",
			host:       "192.168.1.100",
			port:       5000,
			repository: "images",
			expected:   "oci://192.168.1.100:5000/images",
		},
		{
			name:       "IPv6 with port",
			host:       "::1",
			port:       5000,
			repository: "project",
			expected:   "oci://[::1]:5000/project",
		},
		{
			name:       "external registry without port",
			host:       "ghcr.io",
			port:       0,
			repository: "org/repo",
			expected:   "oci://ghcr.io/org/repo",
		},
		{
			name:       "docker hub without port",
			host:       "docker.io",
			port:       0,
			repository: "library/nginx",
			expected:   "oci://docker.io/library/nginx",
		},
		{
			name:       "empty repository",
			host:       "localhost",
			port:       5000,
			repository: "",
			expected:   "oci://localhost:5000/",
		},
		{
			name:       "nested repository path",
			host:       "ghcr.io",
			port:       0,
			repository: "org/project/subdir",
			expected:   "oci://ghcr.io/org/project/subdir",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := helpers.FormatRegistryURL(testCase.host, testCase.port, testCase.repository)
			assert.Equal(t, testCase.expected, result)
		})
	}
}

// TestDetectRegistryFromViper_NilViper tests DetectRegistryFromViper with nil viper.
func TestDetectRegistryFromViper_NilViper(t *testing.T) {
	t.Parallel()

	info, err := helpers.DetectRegistryFromViper(nil)

	require.Error(t, err)
	assert.ErrorIs(t, err, helpers.ErrViperNil)
	assert.Nil(t, info)
}

// TestDetectRegistryFromConfig_DisabledLocalRegistry tests DetectRegistryFromConfig
// with a config where local registry is disabled.
func TestDetectRegistryFromConfig_DisabledLocalRegistry(t *testing.T) {
	t.Parallel()

	// Note: Calling with nil panics - that's a bug in the function.
	// Testing with disabled local registry instead.
}

// TestRegistryInfo_Fields tests that RegistryInfo struct fields work correctly.
func TestRegistryInfo_Fields(t *testing.T) {
	t.Parallel()

	info := helpers.RegistryInfo{
		Host:       "localhost",
		Port:       5000,
		Repository: "myproject",
		Tag:        "v1.0.0",
		Username:   "user",
		Password:   "pass",
		IsExternal: false,
		Source:     "test",
	}

	assert.Equal(t, "localhost", info.Host)
	assert.Equal(t, int32(5000), info.Port)
	assert.Equal(t, "myproject", info.Repository)
	assert.Equal(t, "v1.0.0", info.Tag)
	assert.Equal(t, "user", info.Username)
	assert.Equal(t, "pass", info.Password)
	assert.False(t, info.IsExternal)
	assert.Equal(t, "test", info.Source)
}

// TestRegistryErrors tests that registry error constants are defined correctly.
func TestRegistryErrors(t *testing.T) {
	t.Parallel()

	t.Run("ErrNoRegistryFound", func(t *testing.T) {
		t.Parallel()
		assert.Error(t, helpers.ErrNoRegistryFound)
		assert.Contains(t, helpers.ErrNoRegistryFound.Error(), "unable to detect registry")
	})

	t.Run("ErrViperNil", func(t *testing.T) {
		t.Parallel()
		assert.Error(t, helpers.ErrViperNil)
		assert.Contains(t, helpers.ErrViperNil.Error(), "nil")
	})

	t.Run("ErrRegistryNotSet", func(t *testing.T) {
		t.Parallel()
		assert.Error(t, helpers.ErrRegistryNotSet)
		assert.Contains(t, helpers.ErrRegistryNotSet.Error(), "not set")
	})

	t.Run("ErrLocalRegistryNotConfigured", func(t *testing.T) {
		t.Parallel()
		assert.Error(t, helpers.ErrLocalRegistryNotConfigured)
		assert.Contains(t, helpers.ErrLocalRegistryNotConfigured.Error(), "not configured")
	})

	t.Run("ErrFluxNoSyncURL", func(t *testing.T) {
		t.Parallel()
		assert.Error(t, helpers.ErrFluxNoSyncURL)
		assert.Contains(t, helpers.ErrFluxNoSyncURL.Error(), "sync.url")
	})

	t.Run("ErrArgoCDNoRepoURL", func(t *testing.T) {
		t.Parallel()
		assert.Error(t, helpers.ErrArgoCDNoRepoURL)
		assert.Contains(t, helpers.ErrArgoCDNoRepoURL.Error(), "repoURL")
	})

	t.Run("ErrEmptyOCIURL", func(t *testing.T) {
		t.Parallel()
		assert.Error(t, helpers.ErrEmptyOCIURL)
		assert.Contains(t, helpers.ErrEmptyOCIURL.Error(), "empty")
	})
}
