package v1alpha1

import (
	"fmt"
	"strings"
)

// EnvironmentConfig defines environment config options for Talos distributions.
type EnvironmentConfig string

const (
	// EnvironmentConfigEnabled enables EnvironmentConfig scaffolding.
	EnvironmentConfigEnabled EnvironmentConfig = "Enabled"
	// EnvironmentConfigDisabled disables EnvironmentConfig scaffolding.
	EnvironmentConfigDisabled EnvironmentConfig = "Disabled"
)

// Set for EnvironmentConfig (pflag.Value interface).
func (ec *EnvironmentConfig) Set(value string) error {
	for _, v := range ValidEnvironmentConfigs() {
		if strings.EqualFold(value, string(v)) {
			*ec = v

			return nil
		}
	}

	return fmt.Errorf(
		"%w: %s (valid options: %s, %s)",
		ErrInvalidEnvironmentConfig,
		value,
		EnvironmentConfigEnabled,
		EnvironmentConfigDisabled,
	)
}

// String returns the string representation of the EnvironmentConfig.
func (ec *EnvironmentConfig) String() string {
	return string(*ec)
}

// Type returns the type of the EnvironmentConfig.
func (ec *EnvironmentConfig) Type() string {
	return "EnvironmentConfig"
}

// Default returns the default value for EnvironmentConfig (Disabled).
func (ec *EnvironmentConfig) Default() any {
	return EnvironmentConfigDisabled
}

// ValidValues returns all valid EnvironmentConfig values as strings.
func (ec *EnvironmentConfig) ValidValues() []string {
	return []string{string(EnvironmentConfigEnabled), string(EnvironmentConfigDisabled)}
}
