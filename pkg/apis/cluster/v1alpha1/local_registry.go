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
	Username string
	Password string
}

// Enabled returns true if the registry is configured (non-empty Registry string).
func (r LocalRegistry) Enabled() bool {
	return strings.TrimSpace(r.Registry) != ""
}

// Parse parses the Registry string into its components.
// Format: [user:pass@]host[:port][/path]
// Credentials can use ${ENV_VAR} placeholders.
func (r LocalRegistry) Parse() ParsedRegistry {
	spec := strings.TrimSpace(r.Registry)
	if spec == "" {
		return ParsedRegistry{
			Host: "localhost",
			Port: DefaultLocalRegistryPort,
		}
	}

	var username, password string

	// Extract credentials if present (user:pass@ prefix)
	if atIdx := strings.Index(spec, "@"); atIdx > 0 {
		credPart := spec[:atIdx]
		spec = spec[atIdx+1:]

		if colonIdx := strings.Index(credPart, ":"); colonIdx > 0 {
			username = credPart[:colonIdx]
			password = credPart[colonIdx+1:]
		} else {
			username = credPart
		}
	}

	// Extract path if present (everything after first /)
	var path string
	if slashIdx := strings.Index(spec, "/"); slashIdx > 0 {
		path = spec[slashIdx+1:]
		spec = spec[:slashIdx]
	}

	// Extract port if present (host:port)
	var host string

	port := DefaultLocalRegistryPort

	colonIdx := strings.LastIndex(spec, ":")
	if colonIdx > 0 {
		host = spec[:colonIdx]
		portStr := spec[colonIdx+1:]

		p, parseErr := strconv.ParseInt(portStr, 10, 32)
		if parseErr == nil && p > 0 {
			port = int32(p)
		} else {
			// Not a valid port, treat as part of host (e.g., IPv6)
			host = spec
		}
	} else {
		host = spec
	}

	if host == "" {
		host = "localhost"
	}

	return ParsedRegistry{
		Host:     host,
		Port:     port,
		Path:     path,
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
