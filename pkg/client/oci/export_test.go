//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions
package oci

// VerifyWithRetry exports verifyWithRetry for testing.
var VerifyWithRetry = verifyWithRetry

// PushWithRetry exports pushWithRetry for testing.
var PushWithRetry = pushWithRetry

// PushFn exports pushFn for testing.
type PushFn = pushFn

// NewManifestLayer exports newManifestLayer for testing.
var NewManifestLayer = newManifestLayer

// CollectManifestFiles exports collectManifestFiles for testing.
var CollectManifestFiles = collectManifestFiles

// ClassifyRegistryError exports classifyRegistryError for testing.
var ClassifyRegistryError = classifyRegistryError

// IsNotFoundError exports isNotFoundError for testing.
var IsNotFoundError = isNotFoundError

// BuildRemoteOptionsWithAuth exports buildRemoteOptionsWithAuth for testing.
var BuildRemoteOptionsWithAuth = buildRemoteOptionsWithAuth
