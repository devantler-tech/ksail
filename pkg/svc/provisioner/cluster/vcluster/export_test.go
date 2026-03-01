//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions
package vclusterprovisioner

// IsTransientCreateErrorForTest exposes isTransientCreateError for unit testing.
var IsTransientCreateErrorForTest = isTransientCreateError

// CreateWithRetryForTest exposes createWithRetry for unit testing.
var CreateWithRetryForTest = createWithRetry

// WaitForNetworkRemovalForTest exposes waitForNetworkRemoval for unit testing.
var WaitForNetworkRemovalForTest = waitForNetworkRemoval

// NetworkExistsFnForTest exposes the networkExistsFn type for unit testing.
type NetworkExistsFnForTest = networkExistsFn

// RemoveNetworkFnForTest exposes the removeNetworkFn type for unit testing.
type RemoveNetworkFnForTest = removeNetworkFn
