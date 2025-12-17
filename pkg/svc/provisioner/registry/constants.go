package registry

// Shared registry constants used across services and CLI layers.
const (
	// LocalRegistryContainerName is the docker container name for the developer registry.
	LocalRegistryContainerName = "local-registry"
	// LocalRegistryClusterHost is the hostname clusters use to reach the local registry.
	LocalRegistryClusterHost = LocalRegistryContainerName
)
