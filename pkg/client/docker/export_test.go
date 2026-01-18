//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions
package docker

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
