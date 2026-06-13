//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions
package cluster

import "github.com/devantler-tech/ksail/v7/pkg/svc/credentials"

// EnvResolver is the default environment-backed credentials resolver, exposed so
// white-box tests can drive detectCloudProvider/detectProviderFromEndpoint
// without importing the credentials package themselves.
var EnvResolver credentials.Resolver = credentials.EnvResolver{}

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

// ServerHasIP exports serverHasIP for testing.
var ServerHasIP = serverHasIP
