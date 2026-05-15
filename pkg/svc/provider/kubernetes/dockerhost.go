package kubernetes

import (
	"fmt"
	"os"
)

// WithRemoteDockerHost sets DOCKER_HOST to point at a port-forwarded DinD Docker API,
// executes callback, then restores the original DOCKER_HOST value.
// This is safe for CLI usage (sequential cluster creation) but is NOT goroutine-safe.
// jscpd:ignore-start
func WithRemoteDockerHost(pf *PortForwardSession, callback func() error) error {
	dockerHost := fmt.Sprintf("tcp://127.0.0.1:%d", pf.LocalPort)

	origHost := os.Getenv("DOCKER_HOST")

	err := os.Setenv("DOCKER_HOST", dockerHost)
	if err != nil {
		return fmt.Errorf("set DOCKER_HOST: %w", err)
	}

	defer func() {
		if origHost == "" {
			_ = os.Unsetenv("DOCKER_HOST")
		} else {
			_ = os.Setenv("DOCKER_HOST", origHost)
		}
	}()

	return callback()
}

// jscpd:ignore-end
