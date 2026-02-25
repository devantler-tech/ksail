//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions
package vclusterprovisioner

// CreateDockerFn is the exported type alias for createDockerFn.
type CreateDockerFn = createDockerFn

// RetryCleanupFn is the exported type alias for retryCleanupFn.
type RetryCleanupFn = retryCleanupFn

// DBusRecoverFn is the exported type alias for dbusRecoverFn.
type DBusRecoverFn = dbusRecoverFn

// IsTransientCreateErrorForTest exposes isTransientCreateError for unit testing.
var IsTransientCreateErrorForTest = isTransientCreateError

// CreateWithRetryForTest exposes createWithRetry for unit testing.
var CreateWithRetryForTest = createWithRetry
