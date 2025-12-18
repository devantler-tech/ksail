package registry

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

// MirrorSpec represents a parsed mirror registry specification entry.
type MirrorSpec struct {
	Host   string
	Remote string
}

// MirrorEntry contains the normalized data required to create a registry mirror.
type MirrorEntry struct {
	Host          string
	SanitizedName string
	ContainerName string
	Endpoint      string
	Port          int
	Remote        string
}

// ParseMirrorSpecs converts raw mirror specification strings into structured specs.
// Invalid entries (missing host or remote) are ignored.
func ParseMirrorSpecs(specs []string) []MirrorSpec {
	parsed := make([]MirrorSpec, 0, len(specs))

	for _, raw := range specs {
		host, remote, ok := splitMirrorSpec(raw)
		if !ok {
			continue
		}

		host = strings.TrimSpace(host)

		remote = strings.TrimSpace(remote)
		if host == "" || remote == "" {
			continue
		}

		parsed = append(parsed, MirrorSpec{
			Host:   host,
			Remote: remote,
		})
	}

	return parsed
}

// BuildMirrorEntries converts mirror specs into registry entries using the provided prefix.
// Prefix should exclude the trailing hyphen (e.g., "kind", "k3d"). An empty prefix results in
// container names that match the sanitized host directly, which is useful when sharing mirrors across distributions.
// Existing hosts are skipped, and the allocations update the provided maps.
func BuildMirrorEntries(
	specs []MirrorSpec,
	containerPrefix string,
	existingHosts map[string]struct{},
	usedPorts map[int]struct{},
	nextPort *int,
) []MirrorEntry {
	targetHosts := existingHosts
	if targetHosts == nil {
		targetHosts = map[string]struct{}{}
	}

	allocatedPorts := usedPorts
	if allocatedPorts == nil {
		allocatedPorts = map[int]struct{}{}
	}

	entries := make([]MirrorEntry, 0, len(specs))

	for _, spec := range specs {
		host := strings.TrimSpace(spec.Host)
		if host == "" {
			continue
		}

		if _, exists := targetHosts[host]; exists {
			continue
		}

		sanitized := SanitizeHostIdentifier(host)
		if sanitized == "" {
			continue
		}

		port := AllocatePort(nextPort, allocatedPorts)

		containerName := sanitized
		if trimmed := strings.TrimSpace(containerPrefix); trimmed != "" {
			containerName = fmt.Sprintf("%s-%s", trimmed, sanitized)
		}

		// Use DefaultRegistryPort for the endpoint since all registry containers
		// listen on port 5000 internally (port is the host-mapped port)
		endpoint := "http://" + net.JoinHostPort(containerName, strconv.Itoa(DefaultRegistryPort))

		entries = append(entries, MirrorEntry{
			Host:          host,
			SanitizedName: sanitized,
			ContainerName: containerName,
			Endpoint:      endpoint,
			Port:          port,
			Remote:        spec.Remote,
		})

		targetHosts[host] = struct{}{}
	}

	return entries
}

// BuildHostEndpointMap merges parsed mirror specifications with existing host endpoint
// mappings. Generated mirror endpoints are tracked internally while upstream remotes are
// appended to preserve fallbacks. Returns the updated map and a boolean indicating
// whether any changes were applied.
func BuildHostEndpointMap(
	specs []MirrorSpec,
	containerPrefix string,
	existing map[string][]string,
) (map[string][]string, bool) {
	hostEndpoints := cloneEndpointMap(existing)

	usedPorts, nextPort := collectUsedPorts(hostEndpoints)

	entries := BuildMirrorEntries(specs, containerPrefix, nil, usedPorts, &nextPort)
	if len(entries) == 0 {
		return hostEndpoints, false
	}

	updated := false

	for _, entry := range entries {
		endpoints, existed := hostEndpoints[entry.Host]
		previousLen := len(endpoints)

		// Add the local mirror endpoint first (for K3d registry.yaml)
		if entry.Endpoint != "" && !containsEndpoint(endpoints, entry.Endpoint) {
			endpoints = append(endpoints, entry.Endpoint)
		}

		if entry.Remote != "" && !containsEndpoint(endpoints, entry.Remote) {
			endpoints = append(endpoints, entry.Remote)
		}

		if len(endpoints) == 0 {
			endpoints = []string{GenerateUpstreamURL(entry.Host)}
		}

		if !existed || len(endpoints) != previousLen {
			updated = true
		}

		hostEndpoints[entry.Host] = endpoints
	}

	return hostEndpoints, updated
}

func cloneEndpointMap(source map[string][]string) map[string][]string {
	if len(source) == 0 {
		return map[string][]string{}
	}

	clone := make(map[string][]string, len(source))
	for host, endpoints := range source {
		copied := make([]string, len(endpoints))
		copy(copied, endpoints)
		clone[host] = copied
	}

	return clone
}

func collectUsedPorts(hostEndpoints map[string][]string) (map[int]struct{}, int) {
	used := make(map[int]struct{})
	next := DefaultRegistryPort

	for _, endpoints := range hostEndpoints {
		for _, endpoint := range endpoints {
			port := ExtractPortFromEndpoint(endpoint)
			if port <= 0 {
				continue
			}

			used[port] = struct{}{}
			if port >= next {
				next = port + 1
			}
		}
	}

	return used, next
}

// InitPortAllocation prepares the used ports set and the next available port based on provided base mapping.
func InitPortAllocation(baseUsedPorts map[int]struct{}) (map[int]struct{}, int) {
	usedPorts := make(map[int]struct{}, len(baseUsedPorts))
	nextPort := DefaultRegistryPort

	for port := range baseUsedPorts {
		usedPorts[port] = struct{}{}

		if port >= nextPort {
			nextPort = port + 1
		}
	}

	return usedPorts, nextPort
}

func containsEndpoint(endpoints []string, candidate string) bool {
	for _, endpoint := range endpoints {
		if strings.TrimSpace(endpoint) == strings.TrimSpace(candidate) {
			return true
		}
	}

	return false
}

// RenderK3dMirrorConfig renders a K3d-compatible mirrors configuration from the provided
// host endpoints mapping. Hosts are sorted deterministically to ensure stable output.
func RenderK3dMirrorConfig(hostEndpoints map[string][]string) string {
	if len(hostEndpoints) == 0 {
		return ""
	}

	hosts := make([]string, 0, len(hostEndpoints))
	for host := range hostEndpoints {
		hosts = append(hosts, host)
	}

	SortHosts(hosts)

	var builder strings.Builder
	builder.WriteString("mirrors:\n")

	for _, host := range hosts {
		endpoints := filterK3dEndpoints(hostEndpoints[host])
		if len(endpoints) == 0 {
			endpoints = []string{GenerateUpstreamURL(host)}
		}

		builder.WriteString("  \"")
		builder.WriteString(host)
		builder.WriteString("\":\n")
		builder.WriteString("    endpoint:\n")

		for _, endpoint := range endpoints {
			builder.WriteString("      - ")
			builder.WriteString(endpoint)
			builder.WriteByte('\n')
		}
	}

	return builder.String()
}

func filterK3dEndpoints(endpoints []string) []string {
	if len(endpoints) == 0 {
		return endpoints
	}

	filtered := make([]string, 0, len(endpoints))

	for _, endpoint := range endpoints {
		trimmed := strings.TrimSpace(endpoint)
		if trimmed == "" {
			continue
		}

		filtered = append(filtered, trimmed)
	}

	return filtered
}

// BuildUpstreamLookup returns a map of registry host to user-specified upstream URL.
func BuildUpstreamLookup(specs []MirrorSpec) map[string]string {
	if len(specs) == 0 {
		return nil
	}

	lookup := make(map[string]string, len(specs))

	for _, spec := range specs {
		host := strings.TrimSpace(spec.Host)

		remote := strings.TrimSpace(spec.Remote)
		if host == "" || remote == "" {
			continue
		}

		lookup[host] = remote
	}

	if len(lookup) == 0 {
		return nil
	}

	return lookup
}

// AllocatePort returns the next available port and updates the tracking map.
func AllocatePort(nextPort *int, usedPorts map[int]struct{}) int {
	if nextPort == nil {
		value := DefaultRegistryPort
		nextPort = &value
	}

	if usedPorts == nil {
		usedPorts = map[int]struct{}{}
	}

	port := *nextPort
	if port <= 0 {
		port = DefaultRegistryPort
	}

	for {
		if _, exists := usedPorts[port]; !exists {
			usedPorts[port] = struct{}{}
			*nextPort = port + 1

			return port
		}

		port++
	}
}

// KindPatch renders a containerd mirror patch for the provided entry.
func KindPatch(entry MirrorEntry) string {
	return fmt.Sprintf(`[plugins."io.containerd.grpc.v1.cri".registry.mirrors."%s"]
  endpoint = ["%s"]`, entry.Host, entry.Endpoint)
}

func splitMirrorSpec(spec string) (string, string, bool) {
	idx := strings.Index(spec, "=")
	if idx <= 0 || idx == len(spec)-1 {
		return "", "", false
	}

	return spec[:idx], spec[idx+1:], true
}
