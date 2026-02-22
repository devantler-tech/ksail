package mirrorregistry

import (
	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	ksailconfigmanager "github.com/devantler-tech/ksail/v5/pkg/fsutil/configmanager/ksail"
	"github.com/spf13/cobra"
)

const (
	// MirrorRegistryFlag is the flag name for mirror-registry configuration.
	MirrorRegistryFlag = "mirror-registry"
)

// DefaultMirrors are the default mirror registries applied when no config or flags are provided.
// These registries are used by KSail's installers:
//   - docker.io: Calico, Gatekeeper, local-path-provisioner, Hetzner CSI
//   - ghcr.io: Flux, Kyverno, kubelet-csr-approver, ArgoCD
//   - quay.io: Cilium, Calico (tigera), ArgoCD, cert-manager
//   - registry.k8s.io: metrics-server, cloud-provider-kind, CSI sidecars
//
//nolint:gochecknoglobals // Exported constant configuration for test access.
var DefaultMirrors = []string{
	"docker.io=https://registry-1.docker.io",
	"ghcr.io=https://ghcr.io",
	"quay.io=https://quay.io",
	"registry.k8s.io=https://registry.k8s.io",
}

// GetMirrorRegistriesWithDefaults returns mirror registries with default values applied.
// This function manually handles mirror-registry flag merging because it's not bound to Viper.
//
// Behavior (REPLACE semantics for flags):
//   - If --mirror-registry flag is explicitly set:
//   - If set to empty string (""): DISABLE (return empty array)
//   - With values: REPLACE (flag values completely override defaults AND config values)
//   - If flag not set:
//   - With config values: use config values from ksail.yaml
//   - Without config values: use defaults (docker.io and ghcr.io) for Docker provider,
//     or empty for cloud providers (Hetzner) since they cannot use local Docker mirrors.
//
// Note: This is intentionally REPLACE semantics, not EXTEND. When a user provides
// --mirror-registry flags, they explicitly specify the complete list of mirrors they want.
func GetMirrorRegistriesWithDefaults(
	cmd *cobra.Command,
	cfgManager *ksailconfigmanager.ConfigManager,
	provider v1alpha1.Provider,
) []string {
	if cmd.Flags().Changed(MirrorRegistryFlag) {
		return getFlagMirrors(cmd)
	}

	return getConfigOrDefaultMirrors(cfgManager, provider)
}

// getFlagMirrors returns mirror registries from explicitly set flags.
// Returns empty slice if user explicitly disabled mirrors with empty string.
func getFlagMirrors(cmd *cobra.Command) []string {
	flagValues, _ := cmd.Flags().GetStringSlice(MirrorRegistryFlag)
	return flagValues
}

// getConfigOrDefaultMirrors returns mirror registries from config or provider-specific defaults.
// Cloud providers like Hetzner cannot access local Docker mirrors, so no defaults are applied.
func getConfigOrDefaultMirrors(
	cfgManager *ksailconfigmanager.ConfigManager,
	provider v1alpha1.Provider,
) []string {
	configValues := cfgManager.Viper.GetStringSlice(MirrorRegistryFlag)
	if len(configValues) > 0 {
		return configValues
	}

	return getProviderDefaultMirrors(provider)
}

// getProviderDefaultMirrors returns default mirrors for providers that support local Docker mirrors.
// Cloud providers (Hetzner) cannot access local Docker containers, so they get no defaults.
func getProviderDefaultMirrors(provider v1alpha1.Provider) []string {
	if provider == v1alpha1.ProviderHetzner {
		return []string{}
	}

	return DefaultMirrors
}
