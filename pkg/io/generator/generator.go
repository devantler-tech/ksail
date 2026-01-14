package generator

import (
	"fmt"
	"net"
	"strconv"
)

// Generator is implemented by specific distribution generators (kind, k3d, kustomization).
// The Options type parameter allows each implementation to define its own options structure.
type Generator[T any, Options any] interface {
	Generate(model T, opts Options) (string, error)
}

// BuildOCIURL constructs the OCI registry URL for GitOps sources.
// For port values:
//   - port > 0: use the specified port (e.g., 5000 for local registries)
//   - port == 0: default to 5000 (backward compatible behavior)
//   - port < 0 (e.g., -1): no port suffix (for external HTTPS registries like ghcr.io)
func BuildOCIURL(host string, port int32, projectName string) string {
	if host == "" {
		host = "ksail-registry.localhost"
	}

	if projectName == "" {
		projectName = "ksail"
	}

	// Negative port means no port needed (external HTTPS registries)
	if port < 0 {
		return fmt.Sprintf("oci://%s/%s", host, projectName)
	}

	// Zero port defaults to 5000 for backward compatibility
	if port == 0 {
		port = 5000
	}

	hostPort := net.JoinHostPort(host, strconv.Itoa(int(port)))

	return fmt.Sprintf("oci://%s/%s", hostPort, projectName)
}
