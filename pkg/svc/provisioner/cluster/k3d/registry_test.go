package k3dprovisioner_test

import (
	"context"
	"testing"

	k3dprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/k3d"
	k3dv1alpha5 "github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractRegistriesFromConfig_NilConfig(t *testing.T) {
	t.Parallel()

	result := k3dprovisioner.ExtractRegistriesFromConfig(nil, "test-cluster")
	assert.Nil(t, result)
}

func TestExtractRegistriesFromConfig_EmptyRegistriesConfig(t *testing.T) {
	t.Parallel()

	simpleCfg := &k3dv1alpha5.SimpleConfig{
		Registries: k3dv1alpha5.SimpleConfigRegistries{
			Config: "",
		},
	}

	result := k3dprovisioner.ExtractRegistriesFromConfig(simpleCfg, "test-cluster")
	assert.Nil(t, result)
}

func TestExtractRegistriesFromConfig_WhitespaceRegistriesConfig(t *testing.T) {
	t.Parallel()

	simpleCfg := &k3dv1alpha5.SimpleConfig{
		Registries: k3dv1alpha5.SimpleConfigRegistries{
			Config: "   \n\t  ",
		},
	}

	result := k3dprovisioner.ExtractRegistriesFromConfig(simpleCfg, "test-cluster")
	assert.Nil(t, result)
}

func TestExtractRegistriesFromConfig_InvalidYAML(t *testing.T) {
	t.Parallel()

	simpleCfg := &k3dv1alpha5.SimpleConfig{
		Registries: k3dv1alpha5.SimpleConfigRegistries{
			Config: "invalid: yaml: structure: [[[",
		},
	}

	result := k3dprovisioner.ExtractRegistriesFromConfig(simpleCfg, "test-cluster")
	assert.Nil(t, result)
}

func TestExtractRegistriesFromConfig_NoMirrors(t *testing.T) {
	t.Parallel()

	simpleCfg := &k3dv1alpha5.SimpleConfig{
		Registries: k3dv1alpha5.SimpleConfigRegistries{
			Config: `
mirrors:
`,
		},
	}

	result := k3dprovisioner.ExtractRegistriesFromConfig(simpleCfg, "test-cluster")
	assert.Nil(t, result)
}

func TestExtractRegistriesFromConfig_SingleMirror(t *testing.T) {
	t.Parallel()

	simpleCfg := &k3dv1alpha5.SimpleConfig{
		Registries: k3dv1alpha5.SimpleConfigRegistries{
			Config: `
mirrors:
  docker.io:
    endpoint:
      - https://registry-1.docker.io
`,
		},
	}

	result := k3dprovisioner.ExtractRegistriesFromConfig(simpleCfg, "test-cluster")
	require.NotNil(t, result)
	require.Len(t, result, 1)
	assert.Equal(t, "docker.io", result[0].Host)
	assert.Equal(t, "test-cluster-docker.io", result[0].Name)
	assert.Equal(t, "https://registry-1.docker.io", result[0].Upstream)
}

func TestExtractRegistriesFromConfig_MultipleMirrors(t *testing.T) {
	t.Parallel()

	simpleCfg := &k3dv1alpha5.SimpleConfig{
		Registries: k3dv1alpha5.SimpleConfigRegistries{
			Config: `
mirrors:
  docker.io:
    endpoint:
      - https://registry-1.docker.io
  ghcr.io:
    endpoint:
      - https://ghcr.io
  quay.io:
    endpoint:
      - https://quay.io
`,
		},
	}

	result := k3dprovisioner.ExtractRegistriesFromConfig(simpleCfg, "test-cluster")
	require.NotNil(t, result)
	require.Len(t, result, 3)

	// Results should be sorted by host
	assert.Equal(t, "docker.io", result[0].Host)
	assert.Equal(t, "ghcr.io", result[1].Host)
	assert.Equal(t, "quay.io", result[2].Host)
}

func TestExtractRegistriesFromConfig_WithNativeRegistry(t *testing.T) {
	t.Parallel()

	simpleCfg := &k3dv1alpha5.SimpleConfig{
		Registries: k3dv1alpha5.SimpleConfigRegistries{
			Create: &k3dv1alpha5.SimpleConfigRegistryCreateConfig{
				Name: "k3d-registry.localhost",
			},
			Config: `
mirrors:
  k3d-registry.localhost:5000:
    endpoint:
      - http://k3d-registry.localhost:5000
  docker.io:
    endpoint:
      - https://registry-1.docker.io
`,
		},
	}

	result := k3dprovisioner.ExtractRegistriesFromConfig(simpleCfg, "test-cluster")
	require.NotNil(t, result)
	// Native registry should be filtered out
	require.Len(t, result, 1)
	assert.Equal(t, "docker.io", result[0].Host)
}

func TestExtractRegistriesFromConfig_MultipleEndpoints(t *testing.T) {
	t.Parallel()

	simpleCfg := &k3dv1alpha5.SimpleConfig{
		Registries: k3dv1alpha5.SimpleConfigRegistries{
			Config: `
mirrors:
  docker.io:
    endpoint:
      - http://test-cluster-registry-docker.io:5000
      - https://registry-1.docker.io
`,
		},
	}

	result := k3dprovisioner.ExtractRegistriesFromConfig(simpleCfg, "test-cluster")
	require.NotNil(t, result)
	require.Len(t, result, 1)
	assert.Equal(t, "docker.io", result[0].Host)
	// Upstream should be the HTTPS endpoint
	assert.Equal(t, "https://registry-1.docker.io", result[0].Upstream)
}

func TestExtractRegistriesFromConfig_OnlyLocalEndpoint(t *testing.T) {
	t.Parallel()

	simpleCfg := &k3dv1alpha5.SimpleConfig{
		Registries: k3dv1alpha5.SimpleConfigRegistries{
			Config: `
mirrors:
  docker.io:
    endpoint:
      - http://test-cluster-registry-docker.io:5000
`,
		},
	}

	result := k3dprovisioner.ExtractRegistriesFromConfig(simpleCfg, "test-cluster")
	require.NotNil(t, result)
	require.Len(t, result, 1)
	assert.Equal(t, "docker.io", result[0].Host)
	// When no HTTPS endpoint, upstream should fall back
	assert.Equal(t, "http://test-cluster-registry-docker.io:5000", result[0].Upstream)
}

func TestExtractRegistriesFromConfig_ClusterNamePrefix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		clusterName string
		wantPrefix  string
	}{
		{
			name:        "simple cluster name",
			clusterName: "dev",
			wantPrefix:  "dev-",
		},
		{
			name:        "cluster name with hyphens",
			clusterName: "my-cluster",
			wantPrefix:  "my-cluster-",
		},
		{
			name:        "empty cluster name",
			clusterName: "",
			wantPrefix:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			simpleCfg := &k3dv1alpha5.SimpleConfig{
				Registries: k3dv1alpha5.SimpleConfigRegistries{
					Config: `
mirrors:
  docker.io:
    endpoint:
      - https://registry-1.docker.io
`,
				},
			}

			result := k3dprovisioner.ExtractRegistriesFromConfig(simpleCfg, tt.clusterName)
			require.NotNil(t, result)
			require.Len(t, result, 1)
			if tt.wantPrefix != "" {
				assert.Contains(t, result[0].Name, tt.wantPrefix)
			}
		})
	}
}

func TestExtractRegistriesFromConfig_PortAllocation(t *testing.T) {
	t.Parallel()

	simpleCfg := &k3dv1alpha5.SimpleConfig{
		Registries: k3dv1alpha5.SimpleConfigRegistries{
			Config: `
mirrors:
  docker.io:
    endpoint:
      - https://registry-1.docker.io
  ghcr.io:
    endpoint:
      - https://ghcr.io
`,
		},
	}

	result := k3dprovisioner.ExtractRegistriesFromConfig(simpleCfg, "test-cluster")
	require.NotNil(t, result)
	require.Len(t, result, 2)

	// Ports should be allocated starting from 5000
	assert.Greater(t, result[0].Port, 0)
	assert.Greater(t, result[1].Port, 0)
	// Ports should be different
	assert.NotEqual(t, result[0].Port, result[1].Port)
}

func TestSetupRegistries_NilConfig(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	err := k3dprovisioner.SetupRegistries(ctx, nil, "test-cluster", nil, nil)
	assert.NoError(t, err)
}

func TestConnectRegistriesToNetwork_NilConfig(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	err := k3dprovisioner.ConnectRegistriesToNetwork(ctx, nil, "test-cluster", nil, nil)
	assert.NoError(t, err)
}

func TestCleanupRegistries_NilConfig(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	err := k3dprovisioner.CleanupRegistries(ctx, nil, "test-cluster", nil, false, nil)
	assert.NoError(t, err)
}
