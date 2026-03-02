//nolint:gochecknoglobals // export_test.go exposes unexported symbols for testing via package-level vars.
package metallbinstaller

// ExportWaitForCRDsWithOptions exposes (*Installer).waitForCRDsWithOptions for testing.
var ExportWaitForCRDsWithOptions = (*Installer).waitForCRDsWithOptions

// ExportEnsureIPAddressPool exposes (*Installer).ensureIPAddressPool for testing.
var ExportEnsureIPAddressPool = (*Installer).ensureIPAddressPool

// ExportEnsureL2Advertisement exposes (*Installer).ensureL2Advertisement for testing.
var ExportEnsureL2Advertisement = (*Installer).ensureL2Advertisement

// ExportIPRange returns the configured IP range of an Installer for testing.
var ExportIPRange = func(i *Installer) string { return i.ipRange }
