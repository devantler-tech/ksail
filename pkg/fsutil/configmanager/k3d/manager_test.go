package k3d_test

import (
	"os"
	"path/filepath"
	"testing"

	configmanager "github.com/devantler-tech/ksail/v5/pkg/fsutil/configmanager"
	"github.com/devantler-tech/ksail/v5/pkg/fsutil/configmanager/k3d"
	v1alpha5 "github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testFilePermissions = 0o600

type testScenario struct {
	Name                string
	ConfigContent       string
	ShouldError         bool
	UseCustomConfigPath bool
	ValidationFunc      func(t *testing.T, config *v1alpha5.SimpleConfig)
}

// validateK3dDefaults validates K3d default configuration.
func validateK3dDefaults(t *testing.T, config *v1alpha5.SimpleConfig) {
	t.Helper()

	assert.Equal(t, "k3d.io/v1alpha5", config.APIVersion)
	assert.Equal(t, "Simple", config.Kind)
}

// assertK3dBasicConfig asserts basic configuration properties for K3d cluster.
func assertK3dBasicConfig(t *testing.T, config *v1alpha5.SimpleConfig, expectedName string) {
	t.Helper()

	assert.NotNil(t, config)
	assert.Equal(t, "k3d.io/v1alpha5", config.APIVersion)
	assert.Equal(t, "Simple", config.Kind)
	assert.Equal(t, expectedName, config.Name)
}

// validateK3dConfig validates K3d configuration with specific values.
func validateK3dConfig(
	expectedName string,
	expectedServers, expectedAgents int,
) func(t *testing.T, config *v1alpha5.SimpleConfig) {
	return func(t *testing.T, config *v1alpha5.SimpleConfig) {
		t.Helper()

		validateK3dDefaults(t, config)
		assert.Equal(t, expectedName, config.Name)
		assert.Equal(t, expectedServers, config.Servers)
		assert.Equal(t, expectedAgents, config.Agents)
	}
}

func setupTestConfigPath(t *testing.T, scenario testScenario) string {
	t.Helper()

	if !scenario.UseCustomConfigPath {
		return "non-existent-config.yaml"
	}

	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.yaml")

	if scenario.ConfigContent == "" {
		return configPath
	}

	err := os.WriteFile(configPath, []byte(scenario.ConfigContent), testFilePermissions)
	require.NoError(t, err)

	return configPath
}

func assertConfigManagerCaches(
	t *testing.T,
	fileName string,
	configContent string,
	newManager func(configPath string) configmanager.ConfigManager[v1alpha5.SimpleConfig],
) {
	t.Helper()

	dir := t.TempDir()
	configPath := filepath.Join(dir, fileName)

	err := os.WriteFile(configPath, []byte(configContent), testFilePermissions)
	require.NoError(t, err, "failed to write config")

	manager := newManager(configPath)

	first, err := manager.Load(configmanager.LoadOptions{})
	require.NoError(t, err, "initial LoadConfig failed")
	require.NotNil(t, first, "expected config to be loaded")

	err = os.WriteFile(configPath, []byte("invalid: yaml: ["), testFilePermissions)
	require.NoError(t, err, "failed to overwrite config")

	second, err := manager.Load(configmanager.LoadOptions{})
	require.NoError(t, err, "expected cached load to succeed")

	require.Same(t, first, second, "expected cached configuration to be reused")
}

// TestNewK3dSimpleConfig tests the NewK3dSimpleConfig constructor.
func TestNewK3dSimpleConfig(t *testing.T) {
	t.Parallel()

	t.Run("with_all_parameters", func(t *testing.T) {
		t.Parallel()

		config := k3d.NewK3dSimpleConfig(
			"test-cluster",
			"k3d.io/v1alpha5",
			"Simple",
		)

		assertK3dBasicConfig(t, config, "test-cluster")
	})

	t.Run("with_empty_name", func(t *testing.T) {
		t.Parallel()

		config := k3d.NewK3dSimpleConfig(
			"",
			"k3d.io/v1alpha5",
			"Simple",
		)

		assert.NotNil(t, config)
		assert.Equal(t, "k3d-default", config.Name)
	})

	t.Run("with_empty_apiVersion_and_kind", func(t *testing.T) {
		t.Parallel()

		config := k3d.NewK3dSimpleConfig(
			"test-cluster",
			"",
			"",
		)

		assertK3dBasicConfig(t, config, "test-cluster")
	})

	t.Run("with_all_empty_values", func(t *testing.T) {
		t.Parallel()

		config := k3d.NewK3dSimpleConfig("", "", "")

		assert.NotNil(t, config)
		assert.Equal(t, "k3d.io/v1alpha5", config.APIVersion)
		assert.Equal(t, "Simple", config.Kind)
		assert.Equal(t, "k3d-default", config.Name)
	})
}

func TestNewConfigManager(t *testing.T) {
	t.Parallel()

	configPath := "/path/to/config.yaml"
	manager := k3d.NewConfigManager(configPath)

	assert.NotNil(t, manager)
}

// TestLoadConfig tests the LoadConfig method with different scenarios.
func TestLoadConfig(t *testing.T) {
	t.Parallel()

	scenarios := []testScenario{
		{
			Name:                "non-existent file",
			ConfigContent:       "",
			UseCustomConfigPath: false,
			ValidationFunc:      validateK3dDefaults,
		},
		{
			Name: "valid config",
			ConfigContent: `apiVersion: k3d.io/v1alpha5
kind: Simple
metadata:
  name: test-cluster
servers: 1
agents: 2`,
			UseCustomConfigPath: true,
			ValidationFunc:      validateK3dConfig("test-cluster", 1, 2),
		},
		{
			Name: "missing TypeMeta",
			ConfigContent: `metadata:
  name: no-typemeta
servers: 3`,
			UseCustomConfigPath: true,
			ValidationFunc:      validateK3dConfig("no-typemeta", 3, 0),
		},
		{
			Name:                "invalid YAML",
			ConfigContent:       "invalid: yaml: content: [",
			UseCustomConfigPath: true,
			ShouldError:         true,
		},
	}

	for _, scenario := range scenarios {
		t.Run(scenario.Name, func(t *testing.T) {
			t.Parallel()

			configPath := setupTestConfigPath(t, scenario)
			manager := k3d.NewConfigManager(configPath)
			config, err := manager.Load(configmanager.LoadOptions{})

			if scenario.ShouldError {
				require.Error(t, err)
				assert.Nil(t, config)

				return
			}

			require.NoError(t, err)
			require.NotNil(t, config)

			if scenario.ValidationFunc != nil {
				scenario.ValidationFunc(t, config)
			}
		})
	}
}

func TestK3dConfigManagerLoadConfig_ReusesExistingConfig(t *testing.T) {
	t.Parallel()

	assertConfigManagerCaches(
		t,
		"k3d.yaml",
		`apiVersion: k3d.io/v1alpha5
kind: Simple
metadata:
  name: cached
`,
		func(configPath string) configmanager.ConfigManager[v1alpha5.SimpleConfig] {
			return k3d.NewConfigManager(configPath)
		},
	)
}
