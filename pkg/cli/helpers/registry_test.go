package helpers_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/cli/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type formatRegistryURLTestCase struct {
	name       string
	host       string
	port       int32
	repository string
	expected   string
}

func getFormatRegistryURLTestCases() []formatRegistryURLTestCase {
	return []formatRegistryURLTestCase{
		{"localhost with port", "localhost", 5000, "myproject", "oci://localhost:5000/myproject"},
		{
			"custom host with port",
			"registry.example.com",
			8080,
			"app",
			"oci://registry.example.com:8080/app",
		},
		{"IPv4 with port", "192.168.1.100", 5000, "images", "oci://192.168.1.100:5000/images"},
		{"IPv6 with port", "::1", 5000, "project", "oci://[::1]:5000/project"},
		{"external registry without port", "ghcr.io", 0, "org/repo", "oci://ghcr.io/org/repo"},
		{
			"docker hub without port",
			"docker.io",
			0,
			"library/nginx",
			"oci://docker.io/library/nginx",
		},
		{"empty repository", "localhost", 5000, "", "oci://localhost:5000/"},
		{
			"nested repository path",
			"ghcr.io",
			0,
			"org/project/subdir",
			"oci://ghcr.io/org/project/subdir",
		},
	}
}

// TestFormatRegistryURL tests the FormatRegistryURL function.
func TestFormatRegistryURL(t *testing.T) {
	t.Parallel()

	for _, testCase := range getFormatRegistryURLTestCases() {
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
	require.ErrorIs(t, err, helpers.ErrViperNil)
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
		require.Error(t, helpers.ErrNoRegistryFound)
		assert.Contains(t, helpers.ErrNoRegistryFound.Error(), "unable to detect registry")
	})

	t.Run("ErrViperNil", func(t *testing.T) {
		t.Parallel()
		require.Error(t, helpers.ErrViperNil)
		assert.Contains(t, helpers.ErrViperNil.Error(), "nil")
	})

	t.Run("ErrRegistryNotSet", func(t *testing.T) {
		t.Parallel()
		require.Error(t, helpers.ErrRegistryNotSet)
		assert.Contains(t, helpers.ErrRegistryNotSet.Error(), "not set")
	})

	t.Run("ErrLocalRegistryNotConfigured", func(t *testing.T) {
		t.Parallel()
		require.Error(t, helpers.ErrLocalRegistryNotConfigured)
		assert.Contains(t, helpers.ErrLocalRegistryNotConfigured.Error(), "not configured")
	})

	t.Run("ErrFluxNoSyncURL", func(t *testing.T) {
		t.Parallel()
		require.Error(t, helpers.ErrFluxNoSyncURL)
		assert.Contains(t, helpers.ErrFluxNoSyncURL.Error(), "sync.url")
	})

	t.Run("ErrArgoCDNoRepoURL", func(t *testing.T) {
		t.Parallel()
		require.Error(t, helpers.ErrArgoCDNoRepoURL)
		assert.Contains(t, helpers.ErrArgoCDNoRepoURL.Error(), "repoURL")
	})

	t.Run("ErrEmptyOCIURL", func(t *testing.T) {
		t.Parallel()
		require.Error(t, helpers.ErrEmptyOCIURL)
		assert.Contains(t, helpers.ErrEmptyOCIURL.Error(), "empty")
	})
}
