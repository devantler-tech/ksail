package cmd

import (
	"fmt"

	"github.com/devantler-tech/ksail-go/pkg/ui/notify"
	"github.com/docker/docker/client"
	"github.com/spf13/cobra"
)

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
				Type: notify.WarningType,
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
