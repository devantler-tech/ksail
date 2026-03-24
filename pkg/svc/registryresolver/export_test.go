//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions
package registryresolver

import "time"

// ParseDockerConfigCredentials exports parseDockerConfigCredentials for testing.
var ParseDockerConfigCredentials = parseDockerConfigCredentials

// ParseOCIURL exports parseOCIURL for benchmarking.
var ParseOCIURL = parseOCIURL

// ParseRegistryFlag exports parseRegistryFlag for benchmarking.
var ParseRegistryFlag = parseRegistryFlag

// ParseHostPortHost exports parseHostPort for benchmarking, returning the host component.
func ParseHostPortHost(s string) string {
	return parseHostPort(s).host
}

// RetryExternalPush exports retryExternalPush for testing.
var RetryExternalPush = retryExternalPush

// SetExternalPushRetryParams overrides retry parameters for testing and returns
// a cleanup function that restores the production defaults.
func SetExternalPushRetryParams(
	maxAttempts int,
	baseWait, maxWait time.Duration,
) func() {
	origMax := externalPushMaxAttempts
	origBase := externalPushRetryBaseWait
	origMaxW := externalPushRetryMaxWait

	externalPushMaxAttempts = maxAttempts
	externalPushRetryBaseWait = baseWait
	externalPushRetryMaxWait = maxWait

	return func() {
		externalPushMaxAttempts = origMax
		externalPushRetryBaseWait = origBase
		externalPushRetryMaxWait = origMaxW
	}
}
