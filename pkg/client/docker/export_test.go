//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions
package docker

import (
	dockertypes "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
)

// Export unexported functions for testing.

// UniqueNonEmpty exports uniqueNonEmpty for testing.
var UniqueNonEmpty = uniqueNonEmpty

// IsNotConnectedError exports isNotConnectedError for testing.
var IsNotConnectedError = isNotConnectedError

// IsClusterNetworkName exports isClusterNetworkName for testing.
var IsClusterNetworkName = isClusterNetworkName

// RegistryAttachedToOtherClusters exports registryAttachedToOtherClusters for testing.
var RegistryAttachedToOtherClusters = registryAttachedToOtherClusters

// DeriveRegistryVolumeName exports deriveRegistryVolumeName for testing.
var DeriveRegistryVolumeName = deriveRegistryVolumeName

// InspectContainer exports inspectContainer for testing.
var InspectContainer = inspectContainer

// DisconnectRegistryNetwork exports disconnectRegistryNetwork for testing.
var DisconnectRegistryNetwork = disconnectRegistryNetwork

// CleanupRegistryVolume exports cleanupRegistryVolume for testing.
var CleanupRegistryVolume = cleanupRegistryVolume

// CleanupOrphanedRegistryVolume exports cleanupOrphanedRegistryVolume for testing.
var CleanupOrphanedRegistryVolume = cleanupOrphanedRegistryVolume

// RemoveRegistryVolume exports removeRegistryVolume for testing.
var RemoveRegistryVolume = removeRegistryVolume

// ExportBuildContainerConfig exports buildContainerConfig for benchmarking.
func (rm *RegistryManager) ExportBuildContainerConfig(
	config RegistryConfig,
) (*dockertypes.Config, error) {
	return rm.buildContainerConfig(config)
}

// ExportBuildHostConfig exports buildHostConfig for benchmarking.
func (rm *RegistryManager) ExportBuildHostConfig(
	config RegistryConfig,
	volumeName string,
) *dockertypes.HostConfig {
	return rm.buildHostConfig(config, volumeName)
}

// ExportBuildNetworkConfig exports buildNetworkConfig for benchmarking.
func (rm *RegistryManager) ExportBuildNetworkConfig(
	config RegistryConfig,
) *network.NetworkingConfig {
	return rm.buildNetworkConfig(config)
}

// ExportResolveVolumeName exports resolveVolumeName for benchmarking.
func (rm *RegistryManager) ExportResolveVolumeName(config RegistryConfig) string {
	return rm.resolveVolumeName(config)
}

// ExportBuildProxyCredentialsEnv exports buildProxyCredentialsEnv for benchmarking.
func (rm *RegistryManager) ExportBuildProxyCredentialsEnv(
	username, password string,
) ([]string, error) {
	return rm.buildProxyCredentialsEnv(username, password)
}
