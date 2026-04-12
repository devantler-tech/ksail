//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions
package ciliuminstaller

// GatewayAPICRDsURLForTest exposes gatewayAPICRDsURL for testing.
var GatewayAPICRDsURLForTest = gatewayAPICRDsURL

// GatewayAPICRDsVersionForTest exposes gatewayAPICRDsVersion for testing.
var GatewayAPICRDsVersionForTest = gatewayAPICRDsVersion

// DefaultCiliumValuesForTest exposes defaultCiliumValues for testing.
var DefaultCiliumValuesForTest = defaultCiliumValues

// TalosCiliumValuesForTest exposes talosCiliumValues for testing.
var TalosCiliumValuesForTest = talosCiliumValues

// DockerCiliumValuesForTest exposes dockerCiliumValues for testing.
var DockerCiliumValuesForTest = dockerCiliumValues
