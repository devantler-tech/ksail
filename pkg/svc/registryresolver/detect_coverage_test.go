//nolint:varnamelen // Table-driven registry resolver tests keep short locals for readability.
package registryresolver_test

import (
	"testing"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/svc/registryresolver"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDetectRegistryFromConfig_EnabledLocalRegistry tests that DetectRegistryFromConfig
// returns correct Info when local registry is enabled.
func TestDetectRegistryFromConfig_EnabledLocalRegistry(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		cfg        *v1alpha1.Cluster
		wantHost   string
		wantPort   int32
		wantSource string
	}{
		{
			name: "localhost with port",
			cfg: &v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						LocalRegistry: v1alpha1.LocalRegistry{
							Registry: "localhost:5000",
						},
					},
				},
			},
			wantHost:   "localhost",
			wantPort:   5000,
			wantSource: "config:ksail.yaml",
		},
		{
			name: "external registry with credentials",
			cfg: &v1alpha1.Cluster{
				Spec: v1alpha1.Spec{
					Cluster: v1alpha1.ClusterSpec{
						LocalRegistry: v1alpha1.LocalRegistry{
							Registry: "myuser:mypass@ghcr.io/org/repo",
						},
					},
				},
			},
			wantSource: "config:ksail.yaml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			info, err := registryresolver.DetectRegistryFromConfig(tt.cfg)

			require.NoError(t, err)
			require.NotNil(t, info)
			assert.Equal(t, tt.wantSource, info.Source)

			if tt.wantHost != "" {
				assert.Equal(t, tt.wantHost, info.Host)
			}

			if tt.wantPort != 0 {
				assert.Equal(t, tt.wantPort, info.Port)
			}
		})
	}
}

// TestDetectRegistryFromViper_WithRegistrySet tests DetectRegistryFromViper with
// a valid registry flag value.
//
//nolint:funlen // Table-driven test coverage is naturally long.
func TestDetectRegistryFromViper_WithRegistrySet(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		registry     string
		wantHost     string
		wantPort     int32
		wantRepo     string
		wantExternal bool
	}{
		{
			name:         "localhost with port",
			registry:     "localhost:5000",
			wantHost:     "localhost",
			wantPort:     5000,
			wantExternal: false,
		},
		{
			name:         "external registry with path",
			registry:     "ghcr.io/myorg/myrepo",
			wantHost:     "ghcr.io",
			wantPort:     0,
			wantRepo:     "myorg/myrepo",
			wantExternal: true,
		},
		{
			name:         "registry with port and path",
			registry:     "registry.example.com:8080/myproject",
			wantHost:     "registry.example.com",
			wantPort:     8080,
			wantRepo:     "myproject",
			wantExternal: true,
		},
		{
			name:         "127.0.0.1 with port",
			registry:     "127.0.0.1:5000",
			wantHost:     "127.0.0.1",
			wantPort:     5000,
			wantExternal: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			v := viper.New()
			v.Set(registryresolver.ViperRegistryKey, tt.registry)

			info, err := registryresolver.DetectRegistryFromViper(v)

			require.NoError(t, err)
			require.NotNil(t, info)
			assert.Equal(t, tt.wantHost, info.Host)
			assert.Equal(t, tt.wantPort, info.Port)
			assert.Equal(t, tt.wantExternal, info.IsExternal)

			if tt.wantRepo != "" {
				assert.Equal(t, tt.wantRepo, info.Repository)
			}

			assert.Equal(t, "flag/env:registry", info.Source)
		})
	}
}

// TestDetectRegistryFromViper_EmptyRegistryKey tests that an empty registry key
// returns ErrRegistryNotSet.
func TestDetectRegistryFromViper_EmptyRegistryKey(t *testing.T) {
	t.Parallel()

	v := viper.New()
	// Don't set the registry key

	info, err := registryresolver.DetectRegistryFromViper(v)

	require.Error(t, err)
	require.ErrorIs(t, err, registryresolver.ErrRegistryNotSet)
	assert.Nil(t, info)
}

// TestParseHostPort_Host verifies parseHostPort correctly parses host portions.
func TestParseHostPort_Host(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		wantHost string
	}{
		{"plain hostname", "ghcr.io", "ghcr.io"},
		{"localhost with port", "localhost:5000", "localhost"},
		{"IP with port", "192.168.1.1:8080", "192.168.1.1"},
		{"hostname without port", "registry.example.com", "registry.example.com"},
		{"hostname with invalid port", "ghcr.io:abc", "ghcr.io:abc"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := registryresolver.ParseHostPortHost(tt.input)
			assert.Equal(t, tt.wantHost, result)
		})
	}
}

// TestParseOCIURL tests the parseOCIURL function through parseRegistryFlag.
//
//nolint:funlen // Table-driven test coverage is naturally long.
func TestParseOCIURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		url          string
		wantHost     string
		wantPort     int32
		wantRepo     string
		wantExternal bool
		wantErr      bool
	}{
		{
			name:         "oci URL with host and path",
			url:          "oci://ghcr.io/org/repo",
			wantHost:     "ghcr.io",
			wantPort:     0,
			wantRepo:     "org/repo",
			wantExternal: true,
		},
		{
			name:         "oci URL with host port and path",
			url:          "oci://localhost:5000/myrepo",
			wantHost:     "localhost",
			wantPort:     5000,
			wantRepo:     "myrepo",
			wantExternal: false,
		},
		{
			name:         "oci URL with only host",
			url:          "oci://ghcr.io",
			wantHost:     "ghcr.io",
			wantPort:     0,
			wantRepo:     "",
			wantExternal: true,
		},
		{
			name:    "empty URL",
			url:     "oci://",
			wantErr: true,
		},
		{
			name:         "URL without oci prefix",
			url:          "ghcr.io/org/repo",
			wantHost:     "ghcr.io",
			wantPort:     0,
			wantRepo:     "org/repo",
			wantExternal: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			info, err := registryresolver.ParseOCIURL(tt.url)

			if tt.wantErr {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)
			require.NotNil(t, info)
			assert.Equal(t, tt.wantHost, info.Host)
			assert.Equal(t, tt.wantPort, info.Port)
			assert.Equal(t, tt.wantRepo, info.Repository)
			assert.Equal(t, tt.wantExternal, info.IsExternal)
		})
	}
}

// TestParseRegistryFlag tests the parseRegistryFlag function.
//
//nolint:funlen // Table-driven test coverage is naturally long.
func TestParseRegistryFlag(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		flag         string
		wantHost     string
		wantPort     int32
		wantRepo     string
		wantUsername string
		wantPassword string
		wantExternal bool
	}{
		{
			name:         "simple localhost with port",
			flag:         "localhost:5000",
			wantHost:     "localhost",
			wantPort:     5000,
			wantExternal: false,
		},
		{
			name:         "external registry with path",
			flag:         "ghcr.io/org/repo",
			wantHost:     "ghcr.io",
			wantRepo:     "org/repo",
			wantExternal: true,
		},
		{
			name:         "credentials with @",
			flag:         "user:pass@ghcr.io/org/repo",
			wantHost:     "ghcr.io",
			wantRepo:     "org/repo",
			wantUsername: "user",
			wantPassword: "pass",
			wantExternal: true,
		},
		{
			name:         "username only (no password)",
			flag:         "user@ghcr.io/org/repo",
			wantHost:     "ghcr.io",
			wantRepo:     "org/repo",
			wantUsername: "user",
			wantPassword: "",
			wantExternal: true,
		},
		{
			name:         "port with path",
			flag:         "registry.example.com:8080/myapp",
			wantHost:     "registry.example.com",
			wantPort:     8080,
			wantRepo:     "myapp",
			wantExternal: true,
		},
		{
			name:         "localhost.domain suffix",
			flag:         "registry.localhost:5000",
			wantHost:     "registry.localhost",
			wantPort:     5000,
			wantExternal: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			info := registryresolver.ParseRegistryFlag(tt.flag)

			require.NotNil(t, info)
			assert.Equal(t, tt.wantHost, info.Host)
			assert.Equal(t, tt.wantPort, info.Port)
			assert.Equal(t, tt.wantExternal, info.IsExternal)

			if tt.wantRepo != "" {
				assert.Equal(t, tt.wantRepo, info.Repository)
			}

			if tt.wantUsername != "" {
				assert.Equal(t, tt.wantUsername, info.Username)
			}

			if tt.wantPassword != "" {
				assert.Equal(t, tt.wantPassword, info.Password)
			}
		})
	}
}
