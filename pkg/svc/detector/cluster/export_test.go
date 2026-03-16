//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions
package cluster

// ExtractHostFromURL exports extractHostFromURL for testing.
var ExtractHostFromURL = extractHostFromURL

// IsLocalhost exports isLocalhost for testing.
var IsLocalhost = isLocalhost

// DetectCloudProvider exports detectCloudProvider for testing.
var DetectCloudProvider = detectCloudProvider

// DetectProviderFromEndpoint exports detectProviderFromEndpoint for testing.
var DetectProviderFromEndpoint = detectProviderFromEndpoint
