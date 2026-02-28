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

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			provisioner := k3dprovisioner.CreateProvisioner(testCase.config, testCase.configPath)

			require.NotNil(t, provisioner, "CreateProvisioner should never return nil")
			assert.IsType(t, &k3dprovisioner.Provisioner{}, provisioner)
			assert.Equal(t, testCase.config, provisioner.ExportSimpleCfg())
			assert.Equal(t, testCase.configPath, provisioner.ExportConfigPath())
		})
	}
}

func TestCreateProvisioner_PreservesConfig(t *testing.T) {
	t.Parallel()

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
	assert.Equal(t, config, provisioner.ExportSimpleCfg())
	assert.Equal(t, "/tmp/test-config.yaml", provisioner.ExportConfigPath())
	assert.Equal(t, 2, provisioner.ExportSimpleCfg().Servers)
	assert.Equal(t, 4, provisioner.ExportSimpleCfg().Agents)
	assert.Equal(t, "rancher/k3s:v1.29.0-k3s1", provisioner.ExportSimpleCfg().Image)
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
			assert.Equal(t, testCase.configPath, provisioner.ExportConfigPath())
		})
	}
}
