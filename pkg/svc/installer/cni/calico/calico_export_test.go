//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions
package calicoinstaller

// TalosCalicoValuesForTest exposes talosCalicoValues for testing.
var TalosCalicoValuesForTest = talosCalicoValues

// DefaultCalicoValuesForTest exposes defaultCalicoValues for testing (haEnabled=false).
var DefaultCalicoValuesForTest = func() map[string]string {
	inst := &Installer{}

	return inst.defaultCalicoValues()
}

// DefaultCalicoValuesHAForTest exposes defaultCalicoValues for testing (haEnabled=true).
var DefaultCalicoValuesHAForTest = func() map[string]string {
	inst := &Installer{haEnabled: true}

	return inst.defaultCalicoValues()
}

// CalicoNamespacesForTest exposes calicoNamespaces for testing.
var CalicoNamespacesForTest = calicoNamespaces

// IsAPIDiscoveryErrorForTest exposes isAPIDiscoveryError for testing.
var IsAPIDiscoveryErrorForTest = isAPIDiscoveryError
