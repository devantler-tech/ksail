package workload

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ociReference represents a parsed OCI artifact reference.
// Format: oci://<host>:<port>/<repository>/<optional-variant>:<ref>
type ociReference struct {
	Host       string
	Port       int32
	Repository string
	Variant    string
	Ref        string
}

// OCI reference parsing constants.
const (
	ociScheme       = "oci://"
	minPathParts    = 2
	minPort         = 1
	maxPort         = 65535
	parseIntBase    = 10
	parseIntBitSize = 32
)

// OCI reference parsing errors.
var (
	errInvalidOCIScheme = errors.New("OCI reference must start with 'oci://'")
	errInvalidOCIFormat = errors.New(
		"invalid OCI reference format; expected oci://<host>:<port>/<repository>[/<variant>]:<ref>",
	)
	errInvalidPort = errors.New("invalid port number in OCI reference")
)

// parseOCIReference parses an OCI reference string into its components.
// Format: oci://<host>:<port>/<repository>/<optional-variant>:<ref>
// Returns nil (not an error) when ref is empty, indicating defaults should be used.
// Examples:
//   - oci://localhost:5111/k8s:dev
//   - oci://localhost:5111/my-app/base:v1.0.0
//   - oci://registry.example.com:443/workloads:latest
//
//nolint:nilnil // Returning nil,nil is intentional to indicate "use defaults"
func parseOCIReference(ref string) (*ociReference, error) {
	if ref == "" {
		return nil, nil
	}

	if !strings.HasPrefix(ref, ociScheme) {
		return nil, errInvalidOCIScheme
	}

	remainder := strings.TrimPrefix(ref, ociScheme)
	if remainder == "" {
		return nil, errInvalidOCIFormat
	}

	result := &ociReference{}

	parts := strings.SplitN(remainder, "/", minPathParts)
	if len(parts) < minPathParts {
		return nil, errInvalidOCIFormat
	}

	hostPort := parts[0]
	pathAndRef := parts[1]

	err := parseHostPort(result, hostPort)
	if err != nil {
		return nil, err
	}

	parsePathAndRef(result, pathAndRef)

	return result, nil
}

// parseHostPort extracts host and port from a "host:port" string.
func parseHostPort(result *ociReference, hostPort string) error {
	colonIdx := strings.LastIndex(hostPort, ":")
	if colonIdx == -1 {
		result.Host = hostPort
		result.Port = 0

		return nil
	}

	result.Host = hostPort[:colonIdx]
	portStr := hostPort[colonIdx+1:]

	port, err := strconv.ParseInt(portStr, parseIntBase, parseIntBitSize)
	if err != nil || port < minPort || port > maxPort {
		return fmt.Errorf("%w: %s", errInvalidPort, portStr)
	}

	result.Port = int32(port)

	return nil
}

// parsePathAndRef extracts repository, variant, and ref from the path portion.
func parsePathAndRef(result *ociReference, pathAndRef string) {
	colonIdx := strings.LastIndex(pathAndRef, ":")
	if colonIdx == -1 {
		result.Repository, result.Variant = splitRepositoryPath(pathAndRef)
		result.Ref = ""

		return
	}

	repoPath := pathAndRef[:colonIdx]
	result.Ref = pathAndRef[colonIdx+1:]
	result.Repository, result.Variant = splitRepositoryPath(repoPath)
}

// splitRepositoryPath splits a path into repository and optional variant.
// Examples:
//   - "k8s" -> "k8s", ""
//   - "my-app/base" -> "my-app", "base"
func splitRepositoryPath(path string) (string, string) {
	slashIdx := strings.Index(path, "/")
	if slashIdx == -1 {
		return path, ""
	}

	return path[:slashIdx], path[slashIdx+1:]
}

// String returns the full OCI reference string.
func (r *ociReference) String() string {
	var builder strings.Builder

	builder.WriteString("oci://")
	builder.WriteString(r.Host)

	if r.Port > 0 {
		builder.WriteString(":")
		builder.WriteString(strconv.Itoa(int(r.Port)))
	}

	builder.WriteString("/")
	builder.WriteString(r.Repository)

	if r.Variant != "" {
		builder.WriteString("/")
		builder.WriteString(r.Variant)
	}

	if r.Ref != "" {
		builder.WriteString(":")
		builder.WriteString(r.Ref)
	}

	return builder.String()
}

// FullRepository returns the complete repository path including variant.
func (r *ociReference) FullRepository() string {
	if r.Variant != "" {
		return r.Repository + "/" + r.Variant
	}

	return r.Repository
}
