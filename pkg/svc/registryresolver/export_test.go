//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions
package registryresolver

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
