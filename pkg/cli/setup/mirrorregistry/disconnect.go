package mirrorregistry

import (
	"fmt"

	dockerclient "github.com/devantler-tech/ksail/v7/pkg/client/docker"
	"github.com/docker/docker/client"
	"github.com/spf13/cobra"
)

// DisconnectRegistriesFromNetwork disconnects all registries from a network.
// This is used for Talos which needs registries disconnected BEFORE cluster deletion.
func DisconnectRegistriesFromNetwork(
	cmd *cobra.Command,
	networkName string,
	cleanupDeps CleanupDependencies,
) error {
	return cleanupDeps.DockerInvoker(cmd, func(dockerClient client.APIClient) error {
		registryMgr, mgrErr := dockerclient.NewRegistryManager(dockerClient)
		if mgrErr != nil {
			return fmt.Errorf("failed to create registry manager: %w", mgrErr)
		}

		_, disconnectErr := registryMgr.DisconnectAllFromNetwork(cmd.Context(), networkName)
		if disconnectErr != nil {
			return fmt.Errorf(
				"failed to disconnect registries from network %s: %w",
				networkName,
				disconnectErr,
			)
		}

		return nil
	})
}
