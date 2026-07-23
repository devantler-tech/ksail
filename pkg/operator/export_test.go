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
	// RunInstallers exposes runInstallers for testing.
	RunInstallers = runInstallers
	// RunUninstallers exposes runUninstallers for testing.
	RunUninstallers = runUninstallers
	// RemovedComponentInstallers exposes removedComponentInstallers for testing.
	RemovedComponentInstallers = removedComponentInstallers
	// NewInstallerFactory exposes the operator-owned component factory for testing.
	NewInstallerFactory = newInstallerFactory
	// RecordAWSLoadBalancerControllerOwnership exposes outcome-based operator ownership capture.
	RecordAWSLoadBalancerControllerOwnership = recordAWSLoadBalancerControllerOwnership
	// RecordAWSLoadBalancerControllerOwnershipAfterApply exposes partial-apply ownership capture.
	RecordAWSLoadBalancerControllerOwnershipAfterApply = recordAWSLoadBalancerControllerOwnershipAfterApply
	// SanitizeForWrite exposes the API write-boundary sanitization for testing.
	SanitizeForWrite = sanitizeForWrite
	// CountReadyNodes exposes countReadyNodes for testing.
	CountReadyNodes = countReadyNodes
)
