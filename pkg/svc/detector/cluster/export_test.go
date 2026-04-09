//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions
package cluster

// ExtractHostFromURL exports extractHostFromURL for testing.
var ExtractHostFromURL = extractHostFromURL

// IsLocalhost exports isLocalhost for testing.
var IsLocalhost = isLocalhost

// IsOmniEndpoint exports isOmniEndpoint for testing.
var IsOmniEndpoint = isOmniEndpoint

// DetectCloudProvider exports detectCloudProvider for testing.
var DetectCloudProvider = detectCloudProvider

// DetectProviderFromEndpoint exports detectProviderFromEndpoint for testing.
var DetectProviderFromEndpoint = detectProviderFromEndpoint

// DetectFromServerURL exports detectFromServerURL for testing.
var DetectFromServerURL = detectFromServerURL
