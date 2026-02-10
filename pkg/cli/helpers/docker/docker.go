package docker

import (
	"fmt"

	dockerclient "github.com/devantler-tech/ksail/v5/pkg/client/docker"
	"github.com/devantler-tech/ksail/v5/pkg/notify"
	"github.com/docker/docker/client"
	"github.com/spf13/cobra"
)

// Resources holds Docker client and registry manager for cleanup.
type Resources struct {
	Client          client.APIClient
	RegistryManager *dockerclient.RegistryManager
}

// NewDockerRegistryManager creates a Docker client and registry manager.
// The caller is responsible for calling Close() on the returned resources.
func NewDockerRegistryManager() (*Resources, error) {
	dockerClient, err := client.NewClientWithOpts(
		client.FromEnv,
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		return nil, fmt.Errorf("create docker client: %w", err)
	}

	registryManager, err := dockerclient.NewRegistryManager(dockerClient)
	if err != nil {
		_ = dockerClient.Close()

		return nil, fmt.Errorf("create registry manager: %w", err)
	}

	return &Resources{
		Client:          dockerClient,
		RegistryManager: registryManager,
	}, nil
}

// Close releases the Docker client resources.
func (r *Resources) Close() {
	if r.Client != nil {
		_ = r.Client.Close()
	}
}

// WithDockerClient creates a Docker client, executes the given operation function, and ensures cleanup.
// The Docker client is automatically closed after the operation completes, regardless of success or failure.
//
// This function is suitable for production use. For testing with mock clients, use WithDockerClientInstance instead.
//
// Returns an error if client creation fails or if the operation function returns an error.
func WithDockerClient(cmd *cobra.Command, operation func(client.APIClient) error) error {
	dockerClient, err := client.NewClientWithOpts(
		client.FromEnv,
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		return fmt.Errorf("failed to create docker client: %w", err)
	}

	return WithDockerClientInstance(cmd, dockerClient, operation)
}

// WithDockerClientInstance executes an operation with a provided Docker client and handles cleanup.
// The client will be closed after the operation completes, even if the operation returns an error.
//
// This function is particularly useful for testing with mock clients, as it allows you to provide
// a pre-configured client instance. Any error during client cleanup is logged but does not cause
// the function to return an error if the operation itself succeeded.
func WithDockerClientInstance(
	cmd *cobra.Command,
	dockerClient client.APIClient,
	operation func(client.APIClient) error,
) error {
	defer func() {
		closeErr := dockerClient.Close()
		if closeErr != nil {
			// Log cleanup error but don't fail the operation
			notify.WriteMessage(notify.Message{
				Type: notify.ErrorType,
				Content: fmt.Sprintf(
					"cleanup warning: failed to close docker client: %v",
					closeErr,
				),
				Writer: cmd.OutOrStdout(),
			})
		}
	}()

	return operation(dockerClient)
}
