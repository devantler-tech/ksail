//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions
package kwokprovisioner

// IsTransientCreateErrorForTest exposes isTransientCreateError for unit testing.
var IsTransientCreateErrorForTest = isTransientCreateError

// CreateWithRetryForTest exposes createWithRetry for unit testing.
var CreateWithRetryForTest = createWithRetry

// TransientCreateErrorsForTest exposes transientCreateErrors for unit testing.
var TransientCreateErrorsForTest = transientCreateErrors

// KwokContainerNamesForTest exposes kwokContainerNames for unit testing.
var KwokContainerNamesForTest = kwokContainerNames

// KwokStateDirForTest exposes kwokStateDir for unit testing.
var KwokStateDirForTest = kwokStateDir

// SetDefaultClusterForTest exposes setDefaultCluster for unit testing.
var SetDefaultClusterForTest = setDefaultCluster

// ResolveNameForTest exposes resolveName for unit testing.
func (p *Provisioner) ResolveNameForTest(name string) string {
	return p.resolveName(name)
}

// ResolveConfigPathForTest exposes resolveConfigPath for unit testing.
func (p *Provisioner) ResolveConfigPathForTest() (string, func(), error) {
	return p.resolveConfigPath()
}
