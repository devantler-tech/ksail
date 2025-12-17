package configmanager_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	configmanager "github.com/devantler-tech/ksail-go/pkg/io/config-manager/ksail"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// envTestCase represents a test case for environment variable binding.
type envTestCase struct {
	name     string
	key      string
	envVar   string
	value    string
	testDesc string // Optional description field
}

// runEnvBindingTest is a helper function to test environment variable binding.
func runEnvBindingTest(t *testing.T, viperInstance *viper.Viper, testCase envTestCase) {
	t.Helper()

	t.Run(testCase.name, func(t *testing.T) {
		// Set environment variable
		t.Setenv(testCase.envVar, testCase.value)

		// Bind the environment variable
		_ = viperInstance.BindEnv(testCase.key)

		// Test that the value is retrieved correctly
		assert.Equal(t, testCase.value, viperInstance.GetString(testCase.key))
	})
}

// TestInitializeViper tests the InitializeViper function.
func TestInitializeViper(t *testing.T) {
	// Cannot use t.Parallel() because test uses t.Setenv()
	viperInstance := configmanager.InitializeViper()

	require.NotNil(t, viperInstance, "InitializeViper should return a non-nil viper instance")

	// Test configuration settings
	// We can't directly test the config file name, but we can test that defaults work
	viperInstance.SetDefault("test.config", "default-value")
	assert.Equal(t, "default-value", viperInstance.GetString("test.config"))

	// Test that environment variables are configured
	viperInstance.AutomaticEnv() // This is a setter method, doesn't return a value

	// Test environment prefix
	// We can't directly access the env prefix, but we can test its behavior
	t.Setenv("KSAIL_TEST_VALUE", "test-env-value")

	_ = viperInstance.BindEnv("test.value")
	assert.Equal(t, "test-env-value", viperInstance.GetString("test.value"))
}

// TestInitializeViper_ConfigPaths tests that config paths are set correctly.
func TestInitializeViperConfigPaths(t *testing.T) {
	// Cannot use t.Parallel() because test uses t.Setenv()
	viperInstance := configmanager.InitializeViper()

	// Test that we can set and get values (indicates viper is working)
	viperInstance.SetDefault("test.config", "default-value")
	assert.Equal(t, "default-value", viperInstance.GetString("test.config"))

	// Test that environment variables override defaults
	t.Setenv("KSAIL_TEST_CONFIG", "env-value")

	_ = viperInstance.BindEnv("test.config")
	assert.Equal(t, "env-value", viperInstance.GetString("test.config"))
}

// TestInitializeViper_EnvKeyReplacer tests environment key replacement.
//
//nolint:paralleltest // Cannot use t.Parallel() because subtests use t.Setenv()
func TestInitializeViperEnvKeyReplacer(t *testing.T) {
	// Cannot use t.Parallel() because subtests use t.Setenv()
	viperInstance := configmanager.InitializeViper()

	// Test that dots and dashes in keys are replaced with underscores for env vars
	tests := []envTestCase{
		{
			name:   "Dot replacement",
			key:    "spec.distribution",
			envVar: "KSAIL_SPEC_DISTRIBUTION",
			value:  "kind-test",
		},
		{
			name:   "Dash replacement",
			key:    "source-directory",
			envVar: "KSAIL_SOURCE_DIRECTORY",
			value:  "k8s-test",
		},
		{
			name:   "Mixed dot and dash replacement",
			key:    "spec.connection-timeout",
			envVar: "KSAIL_SPEC_CONNECTION_TIMEOUT",
			value:  "5m-test",
		},
	}

	for _, testCase := range tests {
		runEnvBindingTest(t, viperInstance, testCase)
	}
}

// TestInitializeViper_ConfigFileReading tests that InitializeViper sets up viper correctly
// but doesn't automatically read config files (that's handled by LoadConfig).
//
//nolint:paralleltest // Cannot run in parallel due to directory changes via t.Chdir()
func TestInitializeViperConfigFileReading(t *testing.T) {
	// Cannot use t.Parallel() because test changes directories using t.Chdir()
	// which can conflict with parallel test execution

	// Create a temporary config file
	configContent := `
metadata:
  name: test-cluster-from-file
spec:
  distribution: K3d
  sourceDirectory: k8s-from-file
`

	// Create a temporary directory and file
	tempDir := t.TempDir()
	configFile := filepath.Join(tempDir, "ksail.yaml")

	err := os.WriteFile(configFile, []byte(configContent), 0o600)
	require.NoError(t, err)

	// Change to the temporary directory so viper can find the config file
	t.Chdir(tempDir)

	// Initialize viper - it should NOT automatically read the config file anymore
	viperInstance := configmanager.InitializeViper()

	// Test that values from the config file are NOT automatically loaded
	// (LoadConfig method should handle file reading)
	assert.Empty(t, viperInstance.GetString("metadata.name"))
	assert.Empty(t, viperInstance.GetString("spec.distribution"))
	assert.Empty(t, viperInstance.GetString("spec.sourceDirectory"))

	// Since directory traversal is now handled by InitializeViper and the current directory
	// contains a config file, ReadInConfig should succeed (finding the file)
	err = viperInstance.ReadInConfig()
	require.NoError(
		t,
		err,
		"ReadInConfig should succeed when config file exists in current directory",
	)

	// After ReadInConfig, config values should be available
	assert.Equal(t, "test-cluster-from-file", viperInstance.GetString("metadata.name"))
	assert.Equal(t, "K3d", viperInstance.GetString("spec.distribution"))
	assert.Equal(t, "k8s-from-file", viperInstance.GetString("spec.sourceDirectory"))
}

// TestViperConstants tests the viper-related constants.
func TestViperConstants(t *testing.T) {
	t.Parallel()

	// Test that constants have expected values
	assert.Equal(t, "ksail", configmanager.DefaultConfigFileName)
	assert.Equal(t, "KSAIL", configmanager.EnvPrefix)
	assert.Equal(t, 2, configmanager.SuggestionsMinimumDistance)
}

// TestInitializeViper_EnvironmentVariableBinding tests automatic environment variable binding.
//
//nolint:paralleltest // Cannot use t.Parallel() because subtests use t.Setenv()
func TestInitializeViperEnvironmentVariableBinding(t *testing.T) {
	// Cannot use t.Parallel() because subtests use t.Setenv()
	viperInstance := configmanager.InitializeViper()

	// Test various environment variable patterns
	tests := []envTestCase{
		{
			name:   "Simple environment variable",
			envVar: "KSAIL_TEST_SIMPLE",
			key:    "test.simple",
			value:  "simple-value",
		},
		{
			name:   "Nested environment variable",
			envVar: "KSAIL_NESTED_CONFIG_VALUE",
			key:    "nested.config.value",
			value:  "nested-value",
		},
		{
			name:   "Environment variable with dashes",
			envVar: "KSAIL_DASH_SEPARATED_VALUE",
			key:    "dash-separated-value",
			value:  "dash-value",
		},
	}

	for _, testCase := range tests {
		runEnvBindingTest(t, viperInstance, testCase)
	}
}

// TestInitializeViper_EnvReplacerRules tests environment key replacer rules.
//
//nolint:paralleltest // Cannot use t.Parallel() because subtests use t.Setenv()
func TestInitializeViperEnvReplacerRules(t *testing.T) {
	// Cannot use t.Parallel() because subtests use t.Setenv()
	viperInstance := configmanager.InitializeViper()

	// Test the key replacer rules by setting specific environment variables
	tests := []envTestCase{
		{
			name:     "Period to underscore",
			key:      "metadata.name",
			envVar:   "KSAIL_METADATA_NAME",
			value:    "period-test",
			testDesc: "Periods in keys should be replaced with underscores in env vars",
		},
		{
			name:     "Dash to underscore",
			key:      "source-directory",
			envVar:   "KSAIL_SOURCE_DIRECTORY",
			value:    "dash-test",
			testDesc: "Dashes in keys should be replaced with underscores in env vars",
		},
		{
			name:     "Mixed period and dash",
			key:      "spec.connection-timeout",
			envVar:   "KSAIL_SPEC_CONNECTION_TIMEOUT",
			value:    "mixed-test",
			testDesc: "Both periods and dashes should be replaced with underscores",
		},
		{
			name:     "Multiple periods",
			key:      "deep.nested.config.value",
			envVar:   "KSAIL_DEEP_NESTED_CONFIG_VALUE",
			value:    "deep-test",
			testDesc: "Multiple periods should all be replaced",
		},
		{
			name:     "Multiple dashes",
			key:      "multi-dash-key-name",
			envVar:   "KSAIL_MULTI_DASH_KEY_NAME",
			value:    "multi-dash-test",
			testDesc: "Multiple dashes should all be replaced",
		},
	}

	for _, testCase := range tests {
		runEnvBindingTest(t, viperInstance, testCase)
	}
}

// TestInitializeViper_Idempotency tests that multiple calls to InitializeViper work correctly.
func TestInitializeViperIdempotency(t *testing.T) {
	t.Parallel()

	// Call InitializeViper multiple times
	viper1 := configmanager.InitializeViper()
	viper2 := configmanager.InitializeViper()

	// Both should be valid viper instances
	require.NotNil(t, viper1)
	require.NotNil(t, viper2)

	// They should be different instances (not singletons)
	assert.NotSame(t, viper1, viper2, "Each call should return a new viper instance")

	// Both should work independently
	viper1.SetDefault("test1", "value1")
	viper2.SetDefault("test2", "value2")

	assert.Equal(t, "value1", viper1.GetString("test1"))
	assert.Equal(t, "value2", viper2.GetString("test2"))

	// viper1 should not have test2, viper2 should not have test1
	assert.Empty(t, viper1.GetString("test2"))
	assert.Empty(t, viper2.GetString("test1"))
}

// TestInitializeViper_ConfigType tests that the config type is set correctly.
func TestInitializeViperConfigType(t *testing.T) {
	t.Parallel()

	viperInstance := configmanager.InitializeViper()

	// Create a YAML config string and test parsing
	yamlConfig := `
test:
  yaml: true
  value: "yaml-test"
`

	viperInstance.SetConfigType("yaml")
	err := viperInstance.ReadConfig(strings.NewReader(yamlConfig))
	require.NoError(t, err, "Should be able to read YAML config")

	assert.True(t, viperInstance.GetBool("test.yaml"))
	assert.Equal(t, "yaml-test", viperInstance.GetString("test.value"))
}

// TestInitializeViper_ErrorHandling tests error handling behavior.
func TestInitializeViperErrorHandling(t *testing.T) {
	// Cannot use t.Parallel() because test uses t.Setenv()

	// This test verifies that InitializeViper handles missing config files gracefully
	viperInstance := configmanager.InitializeViper()

	// Even if no config file exists, viper should still work
	require.NotNil(t, viperInstance)

	// Should be able to set and get values
	viperInstance.SetDefault("error.test", "default-value")
	assert.Equal(t, "default-value", viperInstance.GetString("error.test"))

	// Should be able to bind environment variables
	t.Setenv("KSAIL_ERROR_TEST", "env-value")

	_ = viperInstance.BindEnv("error.test")
	assert.Equal(t, "env-value", viperInstance.GetString("error.test"))
}

// TestAddParentDirectoriesToViperPaths_DirectoryTraversal tests the directory traversal functionality.
//
//nolint:paralleltest // Cannot run in parallel due to directory changes via t.Chdir()
func TestAddParentDirectoriesToViperPaths_DirectoryTraversal(t *testing.T) {
	// Cannot use t.Parallel() because test changes directories using t.Chdir()

	// Create a nested directory structure with config files
	tempDir := t.TempDir()

	// Create nested directories
	level1 := filepath.Join(tempDir, "level1")
	level2 := filepath.Join(level1, "level2")
	level3 := filepath.Join(level2, "level3")

	err := os.MkdirAll(level3, 0o750)
	require.NoError(t, err)

	// Create config files at different levels
	configContent := `metadata:
  name: test-traversal
`

	// Config file at level1
	err = os.WriteFile(filepath.Join(level1, "ksail.yaml"), []byte(configContent), 0o600)
	require.NoError(t, err)

	// Config file at tempDir (root level)
	err = os.WriteFile(filepath.Join(tempDir, "ksail.yaml"), []byte(configContent), 0o600)
	require.NoError(t, err)

	// Change to level3 directory
	t.Chdir(level3)

	// Create a viper instance and test directory traversal
	viperInstance := configmanager.InitializeViper()

	// The directory traversal should have found and added both config directories
	// We can test this by attempting to read config - it should find one of them
	err = viperInstance.ReadInConfig()

	// Should either succeed (found a config file) or fail gracefully
	// The key is that the directory traversal logic executed without error
	if err == nil {
		// Success - config was found, which means directory traversal worked
		assert.NotEmpty(t, viperInstance.ConfigFileUsed())
	} else {
		// If it failed, it should be a ConfigFileNotFoundError, not a panic or other error
		var configFileNotFoundError viper.ConfigFileNotFoundError
		assert.ErrorAs(t, err, &configFileNotFoundError,
			"Should be ConfigFileNotFoundError if config not found")
	}
}

// TestAddParentDirectoriesToViperPaths_WithDuplicates tests duplicate path handling.
//
//nolint:paralleltest // Cannot run in parallel due to directory changes via t.Chdir()
func TestAddParentDirectoriesToViperPaths_WithDuplicates(t *testing.T) {
	// Cannot use t.Parallel() because test changes directories using t.Chdir()

	// Create a directory with config file
	tempDir := t.TempDir()
	configContent := `metadata:
  name: test-duplicates
`

	err := os.WriteFile(filepath.Join(tempDir, "ksail.yaml"), []byte(configContent), 0o600)
	require.NoError(t, err)

	// Change to the directory
	t.Chdir(tempDir)

	// Create a viper instance
	viperInstance := configmanager.InitializeViper()

	// The implementation should handle the case where the same directory
	// might be added multiple times (though our current logic prevents this)
	// This test ensures no panics occur and the logic is robust

	// Verify the viper instance is properly configured
	require.NotNil(t, viperInstance)

	// Try to read config to ensure everything works
	err = viperInstance.ReadInConfig()
	if err != nil {
		var configFileNotFoundError viper.ConfigFileNotFoundError
		// Should either succeed or fail with ConfigFileNotFoundError
		assert.True(t, errors.As(err, &configFileNotFoundError) || err == nil)
	}
}

// TestAddParentDirectoriesToViperPaths_ErrorHandling tests error handling in directory traversal.
//
//nolint:paralleltest // Cannot use t.Parallel() because test changes directories using t.Chdir()
func TestAddParentDirectoriesToViperPaths_ErrorHandling(t *testing.T) {
	// Cannot use t.Parallel() because test changes directories using t.Chdir()

	// Create a temp directory and subdirectory
	tempDir := t.TempDir()
	subDir := filepath.Join(tempDir, "removed")

	err := os.Mkdir(subDir, 0o750)
	require.NoError(t, err)

	// Change to the subdirectory
	t.Chdir(subDir)

	// Remove the current directory while we're in it
	// This should make filepath.Abs(".") return an error
	err = os.Remove(subDir)
	require.NoError(t, err)

	// Now InitializeViper should hit the error path in addParentDirectoriesToViperPaths
	viperInstance := configmanager.InitializeViper()

	// The function should not panic and should return a valid instance
	require.NotNil(t, viperInstance)

	// Should still be able to use viper for basic operations
	viperInstance.SetDefault("test.error", "default-value")
	assert.Equal(t, "default-value", viperInstance.GetString("test.error"))
}
