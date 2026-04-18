package talosprovisioner

// IsRetryableTalosApplyConfigError exposes the unexported helper for tests in
// the talosprovisioner_test package.
//
//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions
var IsRetryableTalosApplyConfigError = isRetryableTalosApplyConfigError
