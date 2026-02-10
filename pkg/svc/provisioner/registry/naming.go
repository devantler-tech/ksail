package registry

import (
	"slices"
	"strconv"
	"strings"

	dockerclient "github.com/devantler-tech/ksail/v5/pkg/client/docker"
)

// SanitizeHostIdentifier converts a registry host string into a Docker-safe identifier while keeping dots intact
// so hosts such as docker.io remain reachable via container name resolution.
func SanitizeHostIdentifier(host string) string {
	sanitized := strings.ReplaceAll(host, "/", "-")
	sanitized = strings.ReplaceAll(sanitized, ":", "-")

	return sanitized
}

// GenerateVolumeName returns a deterministic Docker volume name for the registry.
func GenerateVolumeName(host string) string {
	return SanitizeHostIdentifier(host)
}

// GenerateUpstreamURL attempts to derive the upstream registry URL from the host name.
func GenerateUpstreamURL(host string) string {
	if host == "docker.io" {
		return "https://registry-1.docker.io"
	}

	if strings.HasPrefix(host, "http://") || strings.HasPrefix(host, "https://") {
		return host
	}

	return "https://" + host
}

// ExtractRegistryPort determines a unique host port to expose for the given endpoints.
func ExtractRegistryPort(endpoints []string, usedPorts map[int]struct{}, nextPort *int) int {
	if nextPort == nil {
		defaultPort := dockerclient.DefaultRegistryPort
		nextPort = &defaultPort
	}

	if candidate := firstAvailableEndpointPort(endpoints, usedPorts, nextPort); candidate > 0 {
		return candidate
	}

	port := *nextPort
	for {
		if port <= 0 {
			port = dockerclient.DefaultRegistryPort
		}

		if _, exists := usedPorts[port]; !exists {
			break
		}

		port++
	}

	usedPorts[port] = struct{}{}
	*nextPort = port + 1

	return port
}

func firstAvailableEndpointPort(
	endpoints []string,
	usedPorts map[int]struct{},
	nextPort *int,
) int {
	if len(endpoints) == 0 {
		return 0
	}

	extracted := ExtractPortFromEndpoint(endpoints[0])
	if extracted <= 0 {
		return 0
	}

	if _, exists := usedPorts[extracted]; exists {
		return 0
	}

	usedPorts[extracted] = struct{}{}
	if extracted >= *nextPort {
		*nextPort = extracted + 1
	}

	return extracted
}

// ExtractPortFromEndpoint extracts the port from an endpoint URL. Returns 0 if not found.
func ExtractPortFromEndpoint(endpoint string) int {
	lastColon := strings.LastIndex(endpoint, ":")
	if lastColon < 0 {
		return 0
	}

	portStr := endpoint[lastColon+1:]
	if slashIdx := strings.Index(portStr, "/"); slashIdx >= 0 {
		portStr = portStr[:slashIdx]
	}

	port, err := strconv.Atoi(portStr)
	if err != nil || port <= 0 || port > 65535 {
		return 0
	}

	return port
}

// ResolveRegistryName determines the registry container name from endpoints or falls back to prefix + host.
// Only HTTP endpoints represent local mirror containers; HTTPS endpoints are remote upstreams and should be skipped.
func ResolveRegistryName(host string, endpoints []string, prefix string) string {
	expected := SanitizeHostIdentifier(host)
	expectedWithPrefix := BuildRegistryName(prefix, host)

	for _, endpoint := range endpoints {
		// Skip HTTPS endpoints - they represent remote upstreams, not local mirror containers
		if strings.HasPrefix(endpoint, "https://") {
			continue
		}

		name := ExtractNameFromEndpoint(endpoint)
		if name == "" {
			continue
		}

		// Check if the endpoint name matches either the unprefixed or prefixed expected name
		if expected != "" && strings.EqualFold(name, expected) {
			return name
		}

		if expectedWithPrefix != "" && strings.EqualFold(name, expectedWithPrefix) {
			return name
		}
	}

	return BuildRegistryName(prefix, host)
}

// ExtractNameFromEndpoint extracts the hostname portion from an endpoint URL.
func ExtractNameFromEndpoint(endpoint string) string {
	_, afterScheme, found := strings.Cut(endpoint, "//")
	if !found {
		return ""
	}

	host, _, _ := strings.Cut(afterScheme, ":")

	return host
}

// BuildRegistryName constructs a registry container name from prefix and host.
func BuildRegistryName(prefix, host string) string {
	sanitized := SanitizeHostIdentifier(host)
	// Trim spaces and trailing hyphens from prefix to avoid double hyphens
	trimmedPrefix := strings.TrimRight(strings.TrimSpace(prefix), "-")

	if trimmedPrefix == "" {
		return sanitized
	}

	return trimmedPrefix + "-" + sanitized
}

// BuildRegistryInfo creates an Info populated with derived fields using the supplied prefix for container names.
func BuildRegistryInfo(
	host string,
	endpoints []string,
	port int,
	prefix string,
	upstreamOverride string,
	username string,
	password string,
) Info {
	name := ResolveRegistryName(host, endpoints, prefix)

	upstream := strings.TrimSpace(upstreamOverride)
	if upstream == "" {
		upstream = GenerateUpstreamURL(host)
	}

	volume := GenerateVolumeName(host)

	return Info{
		Host:     host,
		Name:     name,
		Upstream: upstream,
		Port:     port,
		Volume:   volume,
		Username: username,
		Password: password,
	}
}

// SortHosts deterministically sorts registry hostnames.
func SortHosts(hosts []string) {
	slices.Sort(hosts)
}

// CollectRegistryNames extracts registry names from a slice of registry Info structs.
// Empty names are filtered out. This is useful for collecting names before cleanup operations.
func CollectRegistryNames(infos []Info) []string {
	names := make([]string, 0, len(infos))

	for _, reg := range infos {
		name := strings.TrimSpace(reg.Name)
		if name == "" {
			continue
		}

		names = append(names, name)
	}

	return names
}
