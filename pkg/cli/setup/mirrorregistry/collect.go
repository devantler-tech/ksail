package mirrorregistry

import (
	"fmt"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	ksailconfigmanager "github.com/devantler-tech/ksail/v5/pkg/io/config-manager/ksail"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/registry"
	"github.com/spf13/cobra"
)

// CollectMirrorSpecs collects and merges mirror specs from flags and existing config.
// Returns the merged specs, registry names, and any error.
func CollectMirrorSpecs(
	cmd *cobra.Command,
	cfgManager *ksailconfigmanager.ConfigManager,
	mirrorsDir string,
	provider v1alpha1.Provider,
) ([]registry.MirrorSpec, []string, error) {
	// Get mirror registry specs with defaults applied
	mirrors := GetMirrorRegistriesWithDefaults(cmd, cfgManager, provider)
	flagSpecs := registry.ParseMirrorSpecs(mirrors)

	// Try to read existing hosts.toml files.
	existingSpecs, err := registry.ReadExistingHostsToml(mirrorsDir)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read existing hosts configuration: %w", err)
	}

	// Merge specs: flag specs override existing specs
	mirrorSpecs := registry.MergeSpecs(existingSpecs, flagSpecs)

	specs, names := buildMirrorSpecsResult(mirrorSpecs)

	return specs, names, nil
}

// CollectTalosMirrorSpecs collects mirror specs from Talos config and command line flags.
// This extracts mirror hosts from the loaded Talos config bundle which includes any
// mirror-registries.yaml patches that were applied during cluster creation.
func CollectTalosMirrorSpecs(
	cmd *cobra.Command,
	cfgManager *ksailconfigmanager.ConfigManager,
	provider v1alpha1.Provider,
) ([]registry.MirrorSpec, []string) {
	// Get mirror registry specs with defaults applied
	mirrors := GetMirrorRegistriesWithDefaults(cmd, cfgManager, provider)
	flagSpecs := registry.ParseMirrorSpecs(mirrors)

	// Extract mirror hosts from the loaded Talos config
	var talosSpecs []registry.MirrorSpec

	if cfgManager.DistributionConfig != nil && cfgManager.DistributionConfig.Talos != nil {
		talosHosts := cfgManager.DistributionConfig.Talos.ExtractMirrorHosts()
		for _, host := range talosHosts {
			talosSpecs = append(talosSpecs, registry.MirrorSpec{
				Host:   host,
				Remote: registry.GenerateUpstreamURL(host),
			})
		}
	}

	// Merge specs: flag specs override Talos config specs for the same host
	mirrorSpecs := registry.MergeSpecs(talosSpecs, flagSpecs)

	return buildMirrorSpecsResult(mirrorSpecs)
}

// buildMirrorSpecsResult builds the registry names from mirror specs.
// This is a shared helper used by CollectMirrorSpecs and CollectTalosMirrorSpecs.
func buildMirrorSpecsResult(
	mirrorSpecs []registry.MirrorSpec,
) ([]registry.MirrorSpec, []string) {
	if len(mirrorSpecs) == 0 {
		return nil, nil
	}

	// Build registry info to get container names
	entries := registry.BuildMirrorEntries(mirrorSpecs, "", nil, nil, nil)

	registryNames := make([]string, 0, len(entries))
	for _, entry := range entries {
		registryNames = append(registryNames, entry.ContainerName)
	}

	return mirrorSpecs, registryNames
}
