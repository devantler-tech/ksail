package k3dprovisioner

import (
	k3dv1alpha5 "github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
)

// CreateProvisioner creates a K3dClusterProvisioner from a pre-loaded configuration.
// The K3d config should be loaded via the config-manager before calling this function,
// allowing any in-memory modifications (e.g., mirror registries) to be preserved.
//
// Parameters:
//   - k3dConfig: Pre-loaded K3d cluster configuration
//   - distributionConfigPath: Path to the K3d configuration file (needed for cluster operations)
func CreateProvisioner(
	k3dConfig *k3dv1alpha5.SimpleConfig,
	distributionConfigPath string,
) *K3dClusterProvisioner {
	return NewK3dClusterProvisioner(
		k3dConfig,
		distributionConfigPath,
	)
}
