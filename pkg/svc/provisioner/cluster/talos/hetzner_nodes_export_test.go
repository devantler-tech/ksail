package talosprovisioner

// IsRetryableTalosApplyConfigError exposes the unexported helper for tests in
// the talosprovisioner_test package.
//
//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions
var IsRetryableTalosApplyConfigError = isRetryableTalosApplyConfigError

// PatchTalosHostname exposes patchTalosHostname for tests in the
// talosprovisioner_test package.
//
//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions
var PatchTalosHostname = patchTalosHostname

// HetznerNodeName exposes hetznerNodeName for tests in the
// talosprovisioner_test package.
//
//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions
var HetznerNodeName = hetznerNodeName

// HetznerNodeTalosAddress exposes hetznerNodeTalosAddress for tests in the
// talosprovisioner_test package.
//
//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions
var HetznerNodeTalosAddress = hetznerNodeTalosAddress

// DiagnoseUnreachableNode exposes diagnoseUnreachableNode for tests in the
// talosprovisioner_test package.
//
//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions
var DiagnoseUnreachableNode = diagnoseUnreachableNode

// MaxNodeNameLength exposes maxNodeNameLength for tests.
const MaxNodeNameLength = maxNodeNameLength
