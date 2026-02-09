package mirrorregistry_test

import (
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/cli/setup/mirrorregistry"
	ksailconfigmanager "github.com/devantler-tech/ksail/v5/pkg/io/configmanager/ksail"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDefaultMirrorsContainsRequiredRegistries verifies that DefaultMirrors includes all
// registries needed by KSail's installers. This test prevents accidental removal of required
// mirrors from the defaults, which would cause CI rate limiting issues. See issue #2075.
func TestDefaultMirrorsContainsRequiredRegistries(t *testing.T) {
	t.Parallel()

	// These registries are required for KSail's installers to avoid rate limits:
	// - docker.io: Calico, Gatekeeper, local-path-provisioner, Hetzner CSI
	// - ghcr.io: Flux, Kyverno, kubelet-csr-approver, ArgoCD
	// - quay.io: Cilium, Calico (tigera), ArgoCD, cert-manager
	// - registry.k8s.io: metrics-server, cloud-provider-kind, CSI sidecars
	requiredHosts := []string{
		"docker.io",
		"ghcr.io",
		"quay.io",
		"registry.k8s.io",
	}

	// Build a set of hosts from DefaultMirrors
	hostSet := make(map[string]bool)

	for _, mirror := range mirrorregistry.DefaultMirrors {
		// Extract host from "host=url" format
		parts := strings.SplitN(mirror, "=", 2)
		if len(parts) >= 1 {
			hostSet[parts[0]] = true
		}
	}

	// Verify all required hosts are present
	for _, host := range requiredHosts {
		assert.True(
			t,
			hostSet[host],
			"DefaultMirrors must include %s - required for KSail installers (see issue #2075)",
			host,
		)
	}
}

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
		provider       v1alpha1.Provider
		expectedResult []string
	}{
		{
			name:           "Docker: no flag, no config -> defaults",
			flagValues:     nil,
			flagChanged:    false,
			configValues:   nil,
			provider:       v1alpha1.ProviderDocker,
			expectedResult: mirrorregistry.DefaultMirrors,
		},
		{
			name:        "Docker: no flag, with config -> config values",
			flagValues:  nil,
			flagChanged: false,
			configValues: []string{
				"registry.example.com=https://registry.example.com",
			},
			provider: v1alpha1.ProviderDocker,
			expectedResult: []string{
				"registry.example.com=https://registry.example.com",
			},
		},
		{
			name:           "Docker: flag set to empty string -> disabled (empty)",
			flagValues:     []string{""},
			flagChanged:    true,
			configValues:   nil,
			provider:       v1alpha1.ProviderDocker,
			expectedResult: []string{},
		},
		{
			name:           "Docker: flag with values, no config -> flag replaces defaults",
			flagValues:     []string{"gcr.io=https://gcr.io"},
			flagChanged:    true,
			configValues:   nil,
			provider:       v1alpha1.ProviderDocker,
			expectedResult: []string{"gcr.io=https://gcr.io"},
		},
		{
			name:        "Docker: flag with values, with config -> flag replaces all",
			flagValues:  []string{"gcr.io=https://gcr.io"},
			flagChanged: true,
			configValues: []string{
				"docker.io=https://registry-1.docker.io",
			},
			provider: v1alpha1.ProviderDocker,
			expectedResult: []string{
				"gcr.io=https://gcr.io",
			},
		},
		{
			name: "Docker: flag with multiple values, no config -> flag replaces defaults",
			flagValues: []string{
				"gcr.io=https://gcr.io",
				"quay.io=https://quay.io",
			},
			flagChanged:  true,
			configValues: nil,
			provider:     v1alpha1.ProviderDocker,
			expectedResult: []string{
				"gcr.io=https://gcr.io",
				"quay.io=https://quay.io",
			},
		},
		{
			name: "Docker: flag with multiple values, with config -> flag replaces all",
			flagValues: []string{
				"gcr.io=https://gcr.io",
				"quay.io=https://quay.io",
			},
			flagChanged: true,
			configValues: []string{
				"docker.io=https://registry-1.docker.io",
				"ghcr.io=https://ghcr.io",
			},
			provider: v1alpha1.ProviderDocker,
			expectedResult: []string{
				"gcr.io=https://gcr.io",
				"quay.io=https://quay.io",
			},
		},
		{
			name:        "Docker: empty string flag with config -> disabled (empty)",
			flagValues:  []string{""},
			flagChanged: true,
			configValues: []string{
				"docker.io=https://registry-1.docker.io",
			},
			provider:       v1alpha1.ProviderDocker,
			expectedResult: []string{},
		},
		// Hetzner provider tests - defaults should be skipped
		{
			name:           "Hetzner: no flag, no config -> empty (no defaults for cloud)",
			flagValues:     nil,
			flagChanged:    false,
			configValues:   nil,
			provider:       v1alpha1.ProviderHetzner,
			expectedResult: []string{},
		},
		{
			name:        "Hetzner: no flag, with config -> config values",
			flagValues:  nil,
			flagChanged: false,
			configValues: []string{
				"docker.io=https://mirror.gcr.io",
			},
			provider: v1alpha1.ProviderHetzner,
			expectedResult: []string{
				"docker.io=https://mirror.gcr.io",
			},
		},
		{
			name:           "Hetzner: flag with external mirror -> flag values",
			flagValues:     []string{"docker.io=https://mirror.gcr.io"},
			flagChanged:    true,
			configValues:   nil,
			provider:       v1alpha1.ProviderHetzner,
			expectedResult: []string{"docker.io=https://mirror.gcr.io"},
		},
		{
			name:           "Hetzner: flag set to empty string -> disabled (empty)",
			flagValues:     []string{""},
			flagChanged:    true,
			configValues:   nil,
			provider:       v1alpha1.ProviderHetzner,
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

			result := mirrorregistry.GetMirrorRegistriesWithDefaults(
				cmd,
				cfgManager,
				testCase.provider,
			)
			assert.Equal(t, testCase.expectedResult, result)
		})
	}
}
