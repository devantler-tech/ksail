package registry

// Shared registry constants used across services and CLI layers.
const (
	// LocalRegistryContainerName is the docker container name for the developer registry.
	LocalRegistryContainerName = "local-registry"
	// LocalRegistryClusterHost is the hostname clusters use to reach the local registry.
	LocalRegistryClusterHost = LocalRegistryContainerName
	// DefaultLocalArtifactTag is used when no explicit tag is provided for a workload
	// artifact. The "dev" tag is intended only for local development and will
	// typically point to the most recently built image, which is convenient but
	// not suitable for reproducible or production deployments where explicit
	// immutable version tags (for example, semantic versions or digests) should
	// be used instead.
	DefaultLocalArtifactTag = "dev"
)
