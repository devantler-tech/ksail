//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions
package kwokprovisioner

// IsTransientCreateErrorForTest exposes isTransientCreateError for unit testing.
var IsTransientCreateErrorForTest = isTransientCreateError

// CreateWithRetryForTest exposes createWithRetry for unit testing.
var CreateWithRetryForTest = createWithRetry

// KwokCreateFnForTest exposes the kwokCreateFn type for unit testing.
type KwokCreateFnForTest = kwokCreateFn

// KwokCleanupFnForTest exposes the kwokCleanupFn type for unit testing.
type KwokCleanupFnForTest = kwokCleanupFn
