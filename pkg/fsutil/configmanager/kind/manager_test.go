package kind_test

import (
	"os"
	"path/filepath"
	"testing"

	configmanager "github.com/devantler-tech/ksail/v5/pkg/fsutil/configmanager"
	"github.com/devantler-tech/ksail/v5/pkg/fsutil/configmanager/kind"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v1alpha4 "sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

const testFilePermissions = 0o600

type testScenario struct {
	Name                string
	ConfigContent       string
	ShouldError         bool
	UseCustomConfigPath bool
	ValidationFunc      func(t *testing.T, config *v1alpha4.Cluster)
}

// validateKindDefaults validates Kind default configuration.
func validateKindDefaults(t *testing.T, config *v1alpha4.Cluster) {
	t.Helper()

	assert.Equal(t, "kind.x-k8s.io/v1alpha4", config.APIVersion)
	assert.Equal(t, "Cluster", config.Kind)
}

// assertKindBasicConfig asserts basic configuration properties for Kind cluster.
func assertKindBasicConfig(t *testing.T, config *v1alpha4.Cluster, expectedName string) {
	t.Helper()

	assert.NotNil(t, config)
	assert.Equal(t, "kind.x-k8s.io/v1alpha4", config.APIVersion)
	assert.Equal(t, "Cluster", config.Kind)
	assert.Equal(t, expectedName, config.Name)
}

// validateKindConfig validates Kind configuration with specific values.
func validateKindConfig(
	expectedName string,
	expectedNodeCount int,
) func(t *testing.T, config *v1alpha4.Cluster) {
	return func(t *testing.T, config *v1alpha4.Cluster) {
		t.Helper()

		validateKindDefaults(t, config)
		assert.Equal(t, expectedName, config.Name)
		assert.Len(t, config.Nodes, expectedNodeCount)
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
	newManager func(configPath string) configmanager.ConfigManager[v1alpha4.Cluster],
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

// TestNewKindCluster tests the NewKindCluster constructor.
func TestNewKindCluster(t *testing.T) {
	t.Parallel()

	t.Run("with_all_parameters", func(t *testing.T) {
		t.Parallel()

		config := kind.NewKindCluster(
			"test-cluster",
			"kind.x-k8s.io/v1alpha4",
			"Cluster",
		)

		assertKindBasicConfig(t, config, "test-cluster")
	})

	t.Run("with_empty_name", func(t *testing.T) {
		t.Parallel()

		config := kind.NewKindCluster(
			"",
			"kind.x-k8s.io/v1alpha4",
			"Cluster",
		)

		assert.NotNil(t, config)
		assert.Equal(t, "kind", config.Name)
	})

	t.Run("with_empty_apiVersion_and_kind", func(t *testing.T) {
		t.Parallel()

		config := kind.NewKindCluster(
			"test-cluster",
			"",
			"",
		)

		assertKindBasicConfig(t, config, "test-cluster")
	})

	t.Run("with_all_empty_values", func(t *testing.T) {
		t.Parallel()

		config := kind.NewKindCluster("", "", "")

		assertKindBasicConfig(t, config, "kind")
	})
}

func TestNewConfigManager(t *testing.T) {
	t.Parallel()

	configPath := "/path/to/config.yaml"
	manager := kind.NewConfigManager(configPath)

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
			ValidationFunc:      validateKindDefaults,
		},
		{
			Name: "valid config",
			ConfigContent: `apiVersion: kind.x-k8s.io/v1alpha4
kind: Cluster
name: test-cluster
nodes:
- role: control-plane
- role: worker`,
			UseCustomConfigPath: true,
			ValidationFunc:      validateKindConfig("test-cluster", 2),
		},
		{
			Name: "missing TypeMeta",
			ConfigContent: `name: no-typemeta
nodes:
- role: control-plane`,
			UseCustomConfigPath: true,
			ValidationFunc:      validateKindConfig("no-typemeta", 1),
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
			manager := kind.NewConfigManager(configPath)
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

func TestKindConfigManagerLoadConfig_ReusesExistingConfig(t *testing.T) {
	t.Parallel()

	assertConfigManagerCaches(
		t,
		"kind.yaml",
		`apiVersion: kind.x-k8s.io/v1alpha4
kind: Cluster
name: cached
`,
		func(configPath string) configmanager.ConfigManager[v1alpha4.Cluster] {
			return kind.NewConfigManager(configPath)
		},
	)
}
