package registry

import (
	"net"
	"sort"
	"strconv"
	"strings"
)

// DefaultRegistryPort is the standard port used for container registries.
const DefaultRegistryPort = 5000

// SanitizeHostIdentifier converts a registry host into a filesystem-safe identifier.
// It replaces slashes and colons with dashes.
func SanitizeHostIdentifier(host string) string {
	sanitized := strings.ReplaceAll(host, "/", "-")
	sanitized = strings.ReplaceAll(sanitized, ":", "-")

	return sanitized
}

// GenerateUpstreamURL converts a registry host into its full upstream URL.
// Special handling for docker.io which maps to registry-1.docker.io.
func GenerateUpstreamURL(host string) string {
	if host == "docker.io" {
		return "https://registry-1.docker.io"
	}

	if strings.HasPrefix(host, "http://") || strings.HasPrefix(host, "https://") {
		return host
	}

	return "https://" + host
}

// ExtractPortFromEndpoint parses the port number from a registry endpoint string.
// Returns DefaultRegistryPort if no port is found.
func ExtractPortFromEndpoint(endpoint string) int {
	_, portStr, err := net.SplitHostPort(endpoint)
	if err != nil {
		return DefaultRegistryPort
	}

	port, err := strconv.Atoi(portStr)
	if err != nil {
		return DefaultRegistryPort
	}

	return port
}

// SortHosts sorts a slice of registry host strings in place.
func SortHosts(hosts []string) {
	sort.Strings(hosts)
}
