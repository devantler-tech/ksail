package k3dprovisioner_test

import (
	"testing"

	k3dprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/k3d"
	k3dv1alpha5 "github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateProvisioner_Factory(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		config     *k3dv1alpha5.SimpleConfig
		configPath string
	}{
		{
			name: "with full config",
			config: &k3dv1alpha5.SimpleConfig{
				Servers: 1,
				Agents:  3,
				Image:   "rancher/k3s:v1.30.0-k3s1",
			},
			configPath: "/path/to/config.yaml",
		},
		{
			name:       "nil config",
			config:     nil,
			configPath: "",
		},
		{
			name: "empty config path",
			config: &k3dv1alpha5.SimpleConfig{
				Servers: 1,
				Agents:  2,
			},
			configPath: "",
		},
		{
			name:       "nil config with path",
			config:     nil,
			configPath: "/path/to/config.yaml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			provisioner := k3dprovisioner.CreateProvisioner(tt.config, tt.configPath)

			require.NotNil(t, provisioner, "CreateProvisioner should never return nil")
			assert.IsType(t, &k3dprovisioner.Provisioner{}, provisioner)
		})
	}
}

func TestCreateProvisioner_PreservesConfig(t *testing.T) {
	t.Parallel()

	// Test that CreateProvisioner preserves the configuration passed to it
	// This is important for mirror registry modifications made in-memory

	config := &k3dv1alpha5.SimpleConfig{
		Servers: 2,
		Agents:  4,
		Image:   "rancher/k3s:v1.29.0-k3s1",
		Registries: k3dv1alpha5.SimpleConfigRegistries{
			Config: `
mirrors:
  docker.io:
    endpoint:
      - https://registry-1.docker.io
`,
		},
	}

	provisioner := k3dprovisioner.CreateProvisioner(config, "/tmp/test-config.yaml")

	require.NotNil(t, provisioner)

	// The provisioner should preserve the config, including any
	// in-memory modifications to mirror registries
	// (We can't easily verify this without accessing internal state,
	// but we document the expected behavior)
}

func TestCreateProvisioner_ConfigPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		configPath string
	}{
		{
			name:       "absolute path",
			configPath: "/absolute/path/to/config.yaml",
		},
		{
			name:       "relative path",
			configPath: "relative/path/config.yaml",
		},
		{
			name:       "empty path",
			configPath: "",
		},
		{
			name:       "path with spaces",
			configPath: "/path with spaces/config.yaml",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			config := &k3dv1alpha5.SimpleConfig{
				Servers: 1,
				Agents:  1,
			}

			provisioner := k3dprovisioner.CreateProvisioner(config, testCase.configPath)

			require.NotNil(t, provisioner)
			// The config path is needed for cluster operations like update
			// but is stored internally and not directly accessible
		})
	}
}
