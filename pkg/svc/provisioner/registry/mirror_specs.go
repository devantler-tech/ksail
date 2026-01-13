package registry

import (
	"fmt"
	"net"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	dockerclient "github.com/devantler-tech/ksail/v5/pkg/client/docker"
)

// envVarPattern matches ${VAR_NAME} placeholders for environment variable expansion.
var envVarPattern = regexp.MustCompile(`\$\{([a-zA-Z_][a-zA-Z0-9_]*)\}`)

// MirrorSpec represents a parsed mirror registry specification entry.
type MirrorSpec struct {
	Host     string
	Remote   string
	Username string // Optional: username for registry authentication (supports ${ENV_VAR} placeholders)
	Password string // Optional: password for registry authentication (supports ${ENV_VAR} placeholders)
}

// ResolveCredentials returns the username and password with environment variable placeholders expanded.
// Placeholders use the format ${VAR_NAME}. If a referenced environment variable is not set,
// the placeholder is replaced with an empty string.
//
//nolint:nonamedreturns // Named returns document the returned values for clarity
func (m MirrorSpec) ResolveCredentials() (username, password string) {
	return expandEnvVars(m.Username), expandEnvVars(m.Password)
}

// HasCredentials returns true if the spec has non-empty username or password.
func (m MirrorSpec) HasCredentials() bool {
	return m.Username != "" || m.Password != ""
}

// expandEnvVars replaces ${VAR_NAME} placeholders with their environment variable values.
func expandEnvVars(value string) string {
	if value == "" {
		return value
	}

	return envVarPattern.ReplaceAllStringFunc(value, func(match string) string {
		// Extract variable name from ${VAR_NAME}
		varName := match[2 : len(match)-1]

		return os.Getenv(varName)
	})
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
// Format: [user:pass@]host[=endpoint]
// Credentials can use ${ENV_VAR} placeholders for environment variable expansion.
func ParseMirrorSpecs(specs []string) []MirrorSpec {
	parsed := make([]MirrorSpec, 0, len(specs))

	for _, raw := range specs {
		host, remote, username, password, ok := splitMirrorSpec(raw)
		if !ok {
			continue
		}

		host = strings.TrimSpace(host)

		remote = strings.TrimSpace(remote)
		if host == "" || remote == "" {
			continue
		}

		parsed = append(parsed, MirrorSpec{
			Host:     host,
			Remote:   remote,
			Username: strings.TrimSpace(username),
			Password: strings.TrimSpace(password),
		})
	}

	return parsed
}

// MergeSpecs merges two sets of mirror specs, with flagSpecs taking precedence.
// If the same host appears in both, the version from flagSpecs is used.
// The result is sorted by host to ensure deterministic output.
func MergeSpecs(existingSpecs, flagSpecs []MirrorSpec) []MirrorSpec {
	// If there are flag specs, they take full precedence for those hosts
	// Start with a map of existing specs
	specMap := make(map[string]MirrorSpec)

	for _, spec := range existingSpecs {
		specMap[spec.Host] = spec
	}

	// Override with flag specs
	for _, spec := range flagSpecs {
		specMap[spec.Host] = spec
	}

	// Convert map back to slice
	result := make([]MirrorSpec, 0, len(specMap))
	for _, spec := range specMap {
		result = append(result, spec)
	}

	// Sort by host to ensure deterministic output
	sort.Slice(result, func(i, j int) bool {
		return result[i].Host < result[j].Host
	})

	return result
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

		// Use dockerclient.DefaultRegistryPort for the endpoint since all registry containers
		// listen on port 5000 internally (port is the host-mapped port)
		portStr := strconv.Itoa(dockerclient.DefaultRegistryPort)
		endpoint := "http://" + net.JoinHostPort(containerName, portStr)

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
	next := dockerclient.DefaultRegistryPort

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
	nextPort := dockerclient.DefaultRegistryPort

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
		value := dockerclient.DefaultRegistryPort
		nextPort = &value
	}

	if usedPorts == nil {
		usedPorts = map[int]struct{}{}
	}

	port := *nextPort
	if port <= 0 {
		port = dockerclient.DefaultRegistryPort
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

// BuildRegistryInfosFromSpecs builds registry Info structs from mirror specs directly.
// This is used when provisioners need Info structs for SetupRegistries/CleanupRegistries.
// The prefix is prepended to container names to avoid Docker DNS collisions.
// The upstreams map provides optional overrides for upstream URLs per host.
// Mirror registries do not need host port allocation since they are accessed
// via Docker network by cluster nodes, not from the host.
func BuildRegistryInfosFromSpecs(
	mirrorSpecs []MirrorSpec,
	upstreams map[string]string,
	_ map[int]struct{}, // baseUsedPorts - kept for API compatibility but unused for mirrors
	prefix string,
) []Info {
	registryInfos := make([]Info, 0, len(mirrorSpecs))

	for _, spec := range mirrorSpecs {
		host := strings.TrimSpace(spec.Host)
		if host == "" {
			continue
		}

		// Build container name with prefix to avoid Docker DNS collisions
		containerName := SanitizeHostIdentifier(host)
		if trimmed := strings.TrimSpace(prefix); trimmed != "" {
			containerName = fmt.Sprintf("%s-%s", trimmed, containerName)
		}

		// Use container port 5000 for the endpoint since mirrors are accessed
		// via Docker network, not from the host
		portStr := strconv.Itoa(dockerclient.DefaultRegistryPort)
		endpoint := "http://" + net.JoinHostPort(containerName, portStr)

		// Get upstream URL
		upstream := spec.Remote
		if upstream == "" {
			upstream = GenerateUpstreamURL(host)
		}

		if upstreams != nil && upstreams[host] != "" {
			upstream = upstreams[host]
		}

		// Port 0 indicates no host port binding needed (mirrors use Docker network)
		info := BuildRegistryInfo(host, []string{endpoint}, 0, prefix, upstream)
		registryInfos = append(registryInfos, info)
	}

	return registryInfos
}

// GenerateHostsToml generates a hosts.toml file content for containerd registry configuration.
// This uses the modern hosts directory pattern as documented at:
// https://gardener.cloud/docs/gardener/advanced/containerd-registry-configuration/#hosts-directory-pattern
//
// The generated content includes:
//   - server: The upstream registry URL (fallback when mirrors are unavailable)
//   - host: Local mirror endpoint with pull and resolve capabilities
//
// Example output for docker.io with a local mirror:
//
//	server = "https://registry-1.docker.io"
//
//	[host."http://docker.io:5000"]
//	  capabilities = ["pull", "resolve"]
func GenerateHostsToml(entry MirrorEntry) string {
	var builder strings.Builder

	// Determine the upstream server URL
	upstream := entry.Remote
	if upstream == "" {
		upstream = GenerateUpstreamURL(entry.Host)
	}

	builder.WriteString(fmt.Sprintf("server = %q\n\n", upstream))

	// Add local mirror endpoint configuration
	builder.WriteString(fmt.Sprintf("[host.%q]\n", entry.Endpoint))
	builder.WriteString("  capabilities = [\"pull\", \"resolve\"]\n")

	return builder.String()
}

// splitMirrorSpec extracts components from a mirror specification string.
// Format: [user:pass@]host[=endpoint].
//
//nolint:nonamedreturns // Named returns document the 5 returned components for clarity
func splitMirrorSpec(spec string) (host, remote, username, password string, ok bool) {
	// Examples:
	//   docker.io
	//   docker.io=https://registry-1.docker.io
	//   user:pass@ghcr.io
	//   ${USER}:${PASS}@ghcr.io=https://ghcr.io
	workingSpec := spec

	// Extract credentials if present (user:pass@ prefix)
	if atIdx := strings.Index(workingSpec, "@"); atIdx > 0 {
		credPart := workingSpec[:atIdx]
		workingSpec = workingSpec[atIdx+1:]

		// Parse user:pass from credPart
		if colonIdx := strings.Index(credPart, ":"); colonIdx > 0 {
			username = credPart[:colonIdx]
			password = credPart[colonIdx+1:]
		} else {
			// Only username, no password
			username = credPart
		}
	}

	// Now parse host=endpoint from the remaining part
	host, remote, found := strings.Cut(workingSpec, "=")
	if !found {
		// No '=' found - treat the whole spec as host and auto-generate the remote URL
		host = strings.TrimSpace(workingSpec)
		if host == "" {
			return "", "", "", "", false
		}

		return host, GenerateUpstreamURL(host), username, password, true
	}

	host = strings.TrimSpace(host)
	if host == "" {
		// Empty host (starts with =)
		return "", "", "", "", false
	}

	remote = strings.TrimSpace(remote)
	if remote == "" {
		// Ends with '=' but no remote value
		return "", "", "", "", false
	}

	return host, remote, username, password, true
}

// GenerateScaffoldedHostsToml generates a hosts.toml file content for scaffolded registry mirrors.
// This generates configuration that redirects requests through a local registry container
// that acts as a pull-through cache for the upstream registry.
//
// Parameters:
//   - spec: MirrorSpec containing Host (e.g., "docker.io") and Remote (e.g., "https://registry-1.docker.io")
//
// Example output for docker.io:
//
//	server = "https://registry-1.docker.io"
//
//	[host."http://docker.io:5000"]
//	  capabilities = ["pull", "resolve"]
//
// The local registry container (e.g., docker.io:5000) will be configured with
// REGISTRY_PROXY_REMOTEURL to proxy requests to the upstream.
func GenerateScaffoldedHostsToml(spec MirrorSpec) string {
	var builder strings.Builder

	// The server is the upstream registry URL (fallback if local mirror is unavailable)
	serverURL := spec.Remote
	if serverURL == "" {
		serverURL = GenerateUpstreamURL(spec.Host)
	}

	builder.WriteString(fmt.Sprintf("server = %q\n\n", serverURL))

	// The host block points to the local registry container
	// The container will be named after the registry host (e.g., docker.io:5000)
	localMirrorURL := "http://" + net.JoinHostPort(spec.Host, "5000")
	builder.WriteString(fmt.Sprintf("[host.%q]\n", localMirrorURL))
	builder.WriteString("  capabilities = [\"pull\", \"resolve\"]\n")

	return builder.String()
}
