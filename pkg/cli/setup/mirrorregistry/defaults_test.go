package mirrorregistry_test

import (
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/cli/setup/mirrorregistry"
	ksailconfigmanager "github.com/devantler-tech/ksail/v5/pkg/io/config-manager/ksail"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestCmdWithConfig() (*cobra.Command, *ksailconfigmanager.ConfigManager) {
	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().StringSlice("mirror-registry", []string{}, "")

	v := viper.New()
	cfgManager := &ksailconfigmanager.ConfigManager{Viper: v}

	return cmd, cfgManager
}

//nolint:funlen // Table-driven tests require many test cases.
func TestGetMirrorRegistriesWithDefaults(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		flagValues     []string
		flagChanged    bool
		configValues   []string
		expectedResult []string
	}{
		{
			name:           "no flag, no config -> defaults",
			flagValues:     nil,
			flagChanged:    false,
			configValues:   nil,
			expectedResult: mirrorregistry.DefaultMirrors,
		},
		{
			name:        "no flag, with config -> config values",
			flagValues:  nil,
			flagChanged: false,
			configValues: []string{
				"registry.example.com=https://registry.example.com",
			},
			expectedResult: []string{
				"registry.example.com=https://registry.example.com",
			},
		},
		{
			name:           "flag set to empty string -> disabled (empty)",
			flagValues:     []string{""},
			flagChanged:    true,
			configValues:   nil,
			expectedResult: []string{},
		},
		{
			name:           "flag with values, no config -> flag replaces defaults",
			flagValues:     []string{"gcr.io=https://gcr.io"},
			flagChanged:    true,
			configValues:   nil,
			expectedResult: []string{"gcr.io=https://gcr.io"},
		},
		{
			name:        "flag with values, with config -> flag replaces all",
			flagValues:  []string{"gcr.io=https://gcr.io"},
			flagChanged: true,
			configValues: []string{
				"docker.io=https://registry-1.docker.io",
			},
			expectedResult: []string{
				"gcr.io=https://gcr.io",
			},
		},
		{
			name: "flag with multiple values, no config -> flag replaces defaults",
			flagValues: []string{
				"gcr.io=https://gcr.io",
				"quay.io=https://quay.io",
			},
			flagChanged:  true,
			configValues: nil,
			expectedResult: []string{
				"gcr.io=https://gcr.io",
				"quay.io=https://quay.io",
			},
		},
		{
			name: "flag with multiple values, with config -> flag replaces all",
			flagValues: []string{
				"gcr.io=https://gcr.io",
				"quay.io=https://quay.io",
			},
			flagChanged: true,
			configValues: []string{
				"docker.io=https://registry-1.docker.io",
				"ghcr.io=https://ghcr.io",
			},
			expectedResult: []string{
				"gcr.io=https://gcr.io",
				"quay.io=https://quay.io",
			},
		},
		{
			name:        "empty string flag with config -> disabled (empty)",
			flagValues:  []string{""},
			flagChanged: true,
			configValues: []string{
				"docker.io=https://registry-1.docker.io",
			},
			expectedResult: []string{},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			cmd, cfgManager := newTestCmdWithConfig()

			// Set config values if specified
			if testCase.configValues != nil {
				cfgManager.Viper.Set("mirror-registry", testCase.configValues)
			}

			// Set flag values if changed - use comma-separated string for StringSlice
			if testCase.flagChanged && testCase.flagValues != nil {
				err := cmd.Flags().Set("mirror-registry", strings.Join(testCase.flagValues, ","))
				require.NoError(t, err)
			}

			result := mirrorregistry.GetMirrorRegistriesWithDefaults(cmd, cfgManager)
			assert.Equal(t, testCase.expectedResult, result)
		})
	}
}
