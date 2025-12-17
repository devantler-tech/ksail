package kind

import (
	"fmt"

	configmanager "github.com/devantler-tech/ksail-go/pkg/io/config-manager"
	"github.com/devantler-tech/ksail-go/pkg/io/config-manager/helpers"
	kindvalidator "github.com/devantler-tech/ksail-go/pkg/io/validator/kind"
	"github.com/devantler-tech/ksail-go/pkg/ui/timer"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

// ConfigManager implements configuration management for Kind cluster configurations.
// It provides file-based configuration loading without Viper dependency.
type ConfigManager struct {
	configPath   string
	config       *v1alpha4.Cluster
	configLoaded bool
}

// Compile-time interface compliance verification.
// This ensures ConfigManager properly implements configmanager.ConfigManager[v1alpha4.Cluster].
var _ configmanager.ConfigManager[v1alpha4.Cluster] = (*ConfigManager)(nil)

// NewKindCluster creates a new v1alpha4.Cluster with the specified name and TypeMeta.
// This function provides a canonical way to create Kind clusters with proper field initialization.
// Use empty string for name to create a cluster without a specific name.
func NewKindCluster(name, apiVersion, kind string) *v1alpha4.Cluster {
	// Set default name if empty
	if name == "" {
		name = "kind"
	}

	if apiVersion == "" {
		apiVersion = "kind.x-k8s.io/v1alpha4"
	}

	if kind == "" {
		kind = "Cluster"
	}

	return &v1alpha4.Cluster{
		TypeMeta: v1alpha4.TypeMeta{
			APIVersion: apiVersion,
			Kind:       kind,
		},
		Name: name,
	}
}

// NewConfigManager creates a new configuration manager for Kind cluster configurations.
// configPath specifies the path to the Kind configuration file to load.
func NewConfigManager(configPath string) *ConfigManager {
	return &ConfigManager{
		configPath:   configPath,
		config:       nil,
		configLoaded: false,
	}
}

// LoadConfig loads the Kind configuration from the specified file.
// Returns the loaded config, either freshly loaded or previously cached.
// If the file doesn't exist, returns a default Kind cluster configuration.
// Validates the configuration after loading and returns an error if validation fails.
// The timer parameter is accepted for interface compliance but not currently used.
func (m *ConfigManager) LoadConfig(_ timer.Timer) (*v1alpha4.Cluster, error) {
	// If config is already loaded, return it
	if m.configLoaded {
		return m.config, nil
	}

	config, err := helpers.LoadAndValidateConfig(
		m.configPath,
		func() *v1alpha4.Cluster {
			// Create default with proper APIVersion and Kind
			config := NewKindCluster("", "kind.x-k8s.io/v1alpha4", "Cluster")
			// Apply Kind defaults
			v1alpha4.SetDefaultsCluster(config)

			return config
		},
		kindvalidator.NewValidator(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load Kind config: %w", err)
	}

	m.config = config
	m.configLoaded = true

	return m.config, nil
}
