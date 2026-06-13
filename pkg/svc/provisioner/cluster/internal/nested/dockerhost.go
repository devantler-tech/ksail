package nested

import (
	"fmt"
	"os"
)

// SetDockerHost points DOCKER_HOST at a port-forwarded DinD Docker API on
// localhost:localPort and returns a restore function that reverts DOCKER_HOST to
// its previous value (unsetting it when it was previously unset).
//
// Unlike kubernetesprovider.WithRemoteDockerHost — which scopes DOCKER_HOST to a
// single callback — this leaves DOCKER_HOST set until the caller invokes restore.
// The Talos create flow needs that because the Talos SDK reads DOCKER_HOST across
// many sequential provisioning steps rather than inside one callback, so the
// callback form is not a drop-in. It is safe for CLI usage (sequential cluster
// creation) but is NOT goroutine-safe.
func SetDockerHost(localPort int) (func(), error) {
	dockerHost := fmt.Sprintf("tcp://127.0.0.1:%d", localPort)

	origHost := os.Getenv("DOCKER_HOST")

	err := os.Setenv("DOCKER_HOST", dockerHost)
	if err != nil {
		return func() {}, fmt.Errorf("set DOCKER_HOST: %w", err)
	}

	return func() {
		if origHost == "" {
			_ = os.Unsetenv("DOCKER_HOST")
		} else {
			_ = os.Setenv("DOCKER_HOST", origHost)
		}
	}, nil
}
