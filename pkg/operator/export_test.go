package operator

//nolint:gochecknoglobals // standard export_test.go pattern for testing unexported functions
var (
	// BuildDistributionConfig exposes buildDistributionConfig for testing.
	BuildDistributionConfig = buildDistributionConfig
	// NewScheme exposes newScheme for testing.
	NewScheme = newScheme
	// ManagerOptions exposes managerOptions for testing.
	ManagerOptions = managerOptions
	// IsNodeReady exposes isNodeReady for testing.
	IsNodeReady = isNodeReady
	// ResolveProvider exposes resolveProvider for testing.
	ResolveProvider = resolveProvider
	// ComponentsSupported exposes componentsSupported for testing.
	ComponentsSupported = componentsSupported
	// RunInstallers exposes runInstallers for testing.
	RunInstallers = runInstallers
)
