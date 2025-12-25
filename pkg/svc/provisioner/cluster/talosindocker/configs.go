package talosindockerprovisioner

import (
	"fmt"

	talosconfigmanager "github.com/devantler-tech/ksail/v5/pkg/io/config-manager/talos"
)

// TalosConfigs is an alias to the config-manager's Configs.
// This provides backwards compatibility while delegating to the centralized implementation.
type TalosConfigs = talosconfigmanager.Configs

// LoadConfigs loads Talos machine configurations from patch directories.
// It reads patches from the configured directories (talos/cluster, talos/control-planes,
// talos/workers), generates base configurations, and applies the patches.
//
// The resulting TalosConfigs provides programmatic access to the patched configurations
// for both control-plane and worker nodes.
func (c *TalosInDockerConfig) LoadConfigs() (*TalosConfigs, error) {
	manager := talosconfigmanager.NewConfigManager(
		c.PatchesDir,
		c.ClusterName,
		c.KubernetesVersion,
		c.NetworkCIDR,
	)

	configs, err := manager.LoadConfig(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to load Talos configs: %w", err)
	}

	return configs, nil
}

// LoadConfigsWithPatches loads Talos machine configurations with additional in-memory patches.
// This is useful when you need to apply runtime patches in addition to file-based patches.
//
// The patches are applied in order: file patches first, then additional patches.
func (c *TalosInDockerConfig) LoadConfigsWithPatches(
	additionalPatches []TalosPatch,
) (*TalosConfigs, error) {
	// Convert TalosPatch to talosconfigmanager.Patch
	managerPatches := make([]talosconfigmanager.Patch, 0, len(additionalPatches))
	for _, p := range additionalPatches {
		managerPatches = append(managerPatches, talosconfigmanager.Patch{
			Path:    p.Path,
			Scope:   convertPatchScope(p.Scope),
			Content: p.Content,
		})
	}

	manager := talosconfigmanager.NewConfigManager(
		c.PatchesDir,
		c.ClusterName,
		c.KubernetesVersion,
		c.NetworkCIDR,
	).WithAdditionalPatches(managerPatches)

	configs, err := manager.LoadConfig(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to load Talos configs with patches: %w", err)
	}

	return configs, nil
}

// convertPatchScope converts a local PatchScope to the config-manager's PatchScope.
func convertPatchScope(scope PatchScope) talosconfigmanager.PatchScope {
	switch scope {
	case PatchScopeCluster:
		return talosconfigmanager.PatchScopeCluster
	case PatchScopeControlPlane:
		return talosconfigmanager.PatchScopeControlPlane
	case PatchScopeWorker:
		return talosconfigmanager.PatchScopeWorker
	default:
		return talosconfigmanager.PatchScopeCluster
	}
}
