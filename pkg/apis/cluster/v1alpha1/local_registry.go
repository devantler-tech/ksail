package v1alpha1

import (
	"strconv"
	"strings"

	"github.com/devantler-tech/ksail/v5/pkg/utils/envvar"
)

// ParsedRegistry contains the parsed components of a registry specification.
type ParsedRegistry struct {
	Host     string
	Port     int32
	Path     string
	Tag      string
	Username string
	Password string
}

// Enabled returns true if the registry is configured (non-empty Registry string).
func (r LocalRegistry) Enabled() bool {
	return strings.TrimSpace(r.Registry) != ""
}

// extractCredentials extracts username and password from a credential string.
func extractCredentials(credPart string) (string, string) {
	if colonIdx := strings.Index(credPart, ":"); colonIdx > 0 {
		return credPart[:colonIdx], credPart[colonIdx+1:]
	}

	return credPart, ""
}

// parseHostAndPort parses the host:port portion of a registry spec.
// Returns host, port, and whether an explicit port was provided.
func parseHostAndPort(spec string) (string, int32, bool) {
	colonIdx := strings.LastIndex(spec, ":")
	if colonIdx <= 0 {
		return spec, 0, false
	}

	portStr := spec[colonIdx+1:]

	p, parseErr := strconv.ParseInt(portStr, 10, 32)
	if parseErr == nil && p > 0 {
		return spec[:colonIdx], int32(p), true
	}

	// Not a valid port, treat as part of host (e.g., IPv6)
	return spec, 0, false
}

const localhostHost = "localhost"

// resolveDefaultPort returns the appropriate default port based on host.
func resolveDefaultPort(host string, hasExplicitPort bool) int32 {
	if hasExplicitPort {
		return 0 // Will be set by caller
	}

	if host == localhostHost || host == "127.0.0.1" {
		return DefaultLocalRegistryPort
	}

	// For external hosts, port stays 0 (meaning HTTPS with implicit port 443)
	return 0
}

// Parse parses the Registry string into its components.
// Format: [user:pass@]host[:port][/path[:tag]]
// Credentials can use ${ENV_VAR} placeholders.
//
// Port handling:
//   - If port is explicitly specified, it's used as-is
//   - For localhost/127.0.0.1 without explicit port: defaults to DefaultLocalRegistryPort (5000)
//   - For external hosts without explicit port: returns 0 (indicates HTTPS with implicit port)
//
// Tag handling:
//   - If path ends with :tag, the tag is extracted and stored separately
//   - Example: ghcr.io/org/repo:mytag -> Path="org/repo", Tag="mytag"
func (r LocalRegistry) Parse() ParsedRegistry {
	spec := strings.TrimSpace(r.Registry)
	if spec == "" {
		return ParsedRegistry{
			Host: localhostHost,
			Port: DefaultLocalRegistryPort,
		}
	}

	var username, password string

	// Extract credentials if present (user:pass@ prefix)
	if atIdx := strings.Index(spec, "@"); atIdx > 0 {
		username, password = extractCredentials(spec[:atIdx])
		spec = spec[atIdx+1:]
	}

	// Extract path if present (everything after first /)
	var path, tag string
	if slashIdx := strings.Index(spec, "/"); slashIdx > 0 {
		path = spec[slashIdx+1:]
		spec = spec[:slashIdx]

		// Extract tag from path if present (path:tag format)
		if colonIdx := strings.LastIndex(path, ":"); colonIdx > 0 {
			tag = path[colonIdx+1:]
			path = path[:colonIdx]
		}
	}

	host, port, hasExplicitPort := parseHostAndPort(spec)

	if host == "" {
		host = localhostHost
	}

	// Apply default port only for local registries when no explicit port was provided
	if !hasExplicitPort {
		port = resolveDefaultPort(host, hasExplicitPort)
	}

	return ParsedRegistry{
		Host:     host,
		Port:     port,
		Path:     path,
		Tag:      tag,
		Username: username,
		Password: password,
	}
}

// ResolveCredentials returns the username and password with environment variable placeholders expanded.
// Placeholders use the format ${VAR_NAME}. If a referenced environment variable is not set,
// the placeholder is replaced with an empty string.
//
//nolint:nonamedreturns // Named returns document the returned values for clarity
func (r LocalRegistry) ResolveCredentials() (username, password string) {
	parsed := r.Parse()

	return envvar.Expand(parsed.Username), envvar.Expand(parsed.Password)
}

// HasCredentials returns true if the registry has non-empty username or password configured.
func (r LocalRegistry) HasCredentials() bool {
	parsed := r.Parse()

	return parsed.Username != "" || parsed.Password != ""
}

// IsExternal returns true if this represents an external registry (not localhost).
// External registries are used for cloud providers where a local Docker registry isn't possible.
func (r LocalRegistry) IsExternal() bool {
	parsed := r.Parse()

	return parsed.Host != "" && parsed.Host != "localhost" && parsed.Host != "127.0.0.1"
}

// ResolvedHost returns the registry host from the parsed spec, defaulting to "localhost".
func (r LocalRegistry) ResolvedHost() string {
	return r.Parse().Host
}

// ResolvedPort returns the registry port from the parsed spec, defaulting to DefaultLocalRegistryPort.
func (r LocalRegistry) ResolvedPort() int32 {
	return r.Parse().Port
}

// ResolvedPath returns the registry path from the parsed spec.
func (r LocalRegistry) ResolvedPath() string {
	return r.Parse().Path
}

// ResolvedTag returns the registry tag from the parsed spec, or empty if not specified.
func (r LocalRegistry) ResolvedTag() string {
	return r.Parse().Tag
}
