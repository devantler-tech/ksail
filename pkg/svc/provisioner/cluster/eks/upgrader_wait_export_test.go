package eksprovisioner

// IsTransientPollErrorForTest exposes the unexported classifier so tests in the
// eksprovisioner_test package can assert that the error a timed-out wait returns
// does NOT report itself as a retryable poll blip.
//
//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions
var IsTransientPollErrorForTest = isTransientPollError
