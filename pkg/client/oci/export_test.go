//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions
package oci

// VerifyWithRetry exports verifyWithRetry for testing.
var VerifyWithRetry = verifyWithRetry

// PushWithRetry exports pushWithRetry for testing.
var PushWithRetry = pushWithRetry

// PushFn exports pushFn for testing.
type PushFn = pushFn
