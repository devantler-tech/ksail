package k3d

import (
	"fmt"

	configmanager "github.com/devantler-tech/ksail-go/pkg/io/config-manager"
	"github.com/devantler-tech/ksail-go/pkg/io/config-manager/helpers"
	k3dvalidator "github.com/devantler-tech/ksail-go/pkg/io/validator/k3d"
	"github.com/devantler-tech/ksail-go/pkg/ui/timer"
	"github.com/k3d-io/k3d/v5/pkg/config/types"
	v1alpha5 "github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
)

// ConfigManager implements configuration management for K3d v1alpha5.SimpleConfig configurations.
// It provides file-based configuration loading without Viper dependency.
type ConfigManager struct {
	configPath   string
	config       *v1alpha5.SimpleConfig
	configLoaded bool
}

// Compile-time interface compliance verification.
// This ensures ConfigManager properly implements configmanager.ConfigManager[v1alpha5.SimpleConfig].
var _ configmanager.ConfigManager[v1alpha5.SimpleConfig] = (*ConfigManager)(nil)

// NewK3dSimpleConfig creates a new v1alpha5.SimpleConfig with the specified name and TypeMeta.
// This function provides a canonical way to create K3d clusters with proper field initialization.
// Use empty string for name to create a cluster without a specific name.
func NewK3dSimpleConfig(name, apiVersion, kind string) *v1alpha5.SimpleConfig {
	// Set default name if empty
	if name == "" {
		name = "k3d-default"
	}

	if apiVersion == "" {
		apiVersion = "k3d.io/v1alpha5"
	}

	if kind == "" {
		kind = "Simple"
	}

	return &v1alpha5.SimpleConfig{
		TypeMeta: types.TypeMeta{
			APIVersion: apiVersion,
			Kind:       kind,
		},
		ObjectMeta: types.ObjectMeta{
			Name: name,
		},
	}
}

// NewConfigManager creates a new configuration manager for K3d cluster configurations.
// configPath specifies the path to the K3d configuration file to load.
func NewConfigManager(configPath string) *ConfigManager {
	return &ConfigManager{
		configPath:   configPath,
		config:       nil,
		configLoaded: false,
	}
}

// LoadConfig loads the K3d configuration from the specified file.
// Returns the loaded config, either freshly loaded or previously cached.
// If the file doesn't exist, returns a default K3d cluster configuration.
// Validates the configuration after loading and returns an error if validation fails.
// The timer parameter is accepted for interface compliance but not currently used.
func (m *ConfigManager) LoadConfig(_ timer.Timer) (*v1alpha5.SimpleConfig, error) {
	// If config is already loaded, return it
	if m.configLoaded {
		return m.config, nil
	}

	config, err := helpers.LoadAndValidateConfig(
		m.configPath,
		func() *v1alpha5.SimpleConfig {
			// Create default with proper APIVersion and Kind
			config := NewK3dSimpleConfig("", "k3d.io/v1alpha5", "Simple")

			return config
		},
		k3dvalidator.NewValidator(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load K3d config: %w", err)
	}

	m.config = config
	m.configLoaded = true

	return m.config, nil
}
