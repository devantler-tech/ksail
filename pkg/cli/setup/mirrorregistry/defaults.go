package mirrorregistry

import (
	ksailconfigmanager "github.com/devantler-tech/ksail/v5/pkg/io/config-manager/ksail"
	"github.com/spf13/cobra"
)

const (
	// MirrorRegistryFlag is the flag name for mirror-registry configuration.
	MirrorRegistryFlag = "mirror-registry"
)

// DefaultMirrors are the default mirror registries applied when no config or flags are provided.
//
//nolint:gochecknoglobals // Exported constant configuration for test access.
var DefaultMirrors = []string{
	"docker.io=https://registry-1.docker.io",
	"ghcr.io=https://ghcr.io",
}

// GetMirrorRegistriesWithDefaults returns mirror registries with default values applied.
// This function manually handles mirror-registry flag merging because it's not bound to Viper.
// Behavior:
//   - If --mirror-registry flag is explicitly set:
//   - If set to empty string (""): DISABLE (return empty array)
//   - With values: REPLACE defaults (flag values override both defaults and config)
//   - If flag not set:
//   - With config values: use config values
//   - Without config values: use defaults (docker.io and ghcr.io)
func GetMirrorRegistriesWithDefaults(
	cmd *cobra.Command,
	cfgManager *ksailconfigmanager.ConfigManager,
) []string {
	// Check if the flag was explicitly set by the user
	flagChanged := cmd.Flags().Changed(MirrorRegistryFlag)

	if !flagChanged {
		// Flag not set by user - check config values
		configValues := cfgManager.Viper.GetStringSlice(MirrorRegistryFlag)
		if len(configValues) > 0 {
			return configValues
		}
		// No config value: use defaults
		return DefaultMirrors
	}

	// Flag was explicitly set: get flag values
	flagValues, _ := cmd.Flags().GetStringSlice(MirrorRegistryFlag)

	// Check if user explicitly disabled mirrors with empty string (--mirror-registry "")
	// When --mirror-registry "" is used, the slice becomes empty
	if len(flagValues) == 0 {
		return []string{}
	}

	// Flag with values: REPLACE defaults (and config)
	return flagValues
}
