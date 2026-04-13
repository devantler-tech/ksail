//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions
package calicoinstaller

// TalosCalicoValuesForTest exposes talosCalicoValues for testing.
var TalosCalicoValuesForTest = talosCalicoValues

// DefaultCalicoValuesForTest exposes defaultCalicoValues for testing.
var DefaultCalicoValuesForTest = defaultCalicoValues

// CalicoCRDNamesForTest exposes calicoCRDNames for testing.
var CalicoCRDNamesForTest = calicoCRDNames

// CalicoNamespacesForTest exposes calicoNamespaces for testing.
var CalicoNamespacesForTest = calicoNamespaces

// IsAPIDiscoveryErrorForTest exposes isAPIDiscoveryError for testing.
var IsAPIDiscoveryErrorForTest = isAPIDiscoveryError

// IsCRDEstablishedForTest exposes isCRDEstablished for testing.
var IsCRDEstablishedForTest = isCRDEstablished
