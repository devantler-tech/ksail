package registryresolver

// Exported functions for testing purposes.
// These wrappers allow the _test package to access internal functions.

// ParseDockerConfigCredentials exports parseDockerConfigCredentials for testing.
func ParseDockerConfigCredentials(configData []byte, host string) (string, string) {
	return parseDockerConfigCredentials(configData, host)
}
