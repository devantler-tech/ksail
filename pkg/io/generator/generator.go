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
func BuildOCIURL(host string, port int32, projectName string) string {
	if host == "" {
		host = "ksail-registry.localhost"
	}

	if port == 0 {
		port = 5000
	}

	if projectName == "" {
		projectName = "ksail"
	}

	hostPort := net.JoinHostPort(host, strconv.Itoa(int(port)))

	return fmt.Sprintf("oci://%s/%s", hostPort, projectName)
}
