package containerengineprovisioner

// ContainerEngineProvisioner defines methods for managing container engines.
type ContainerEngineProvisioner interface {
	CheckReady() (bool, error)
}
