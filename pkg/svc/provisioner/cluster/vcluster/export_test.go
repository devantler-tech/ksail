//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions
package vclusterprovisioner

// IsTransientCreateErrorForTest exposes isTransientCreateError for unit testing.
var IsTransientCreateErrorForTest = isTransientCreateError

// CreateWithRetryForTest exposes createWithRetry for unit testing.
var CreateWithRetryForTest = createWithRetry
