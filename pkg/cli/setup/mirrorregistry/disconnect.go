package mirrorregistry

import (
	"fmt"

	dockerclient "github.com/devantler-tech/ksail/v7/pkg/client/docker"
	"github.com/spf13/cobra"
)

// DisconnectRegistriesByInfo disconnects only the given registries from a network.
//
// Prefer this over DisconnectRegistriesFromNetwork whenever the caller knows which registries
// belong to the cluster it is tearing down. Some networks are SHARED between clusters — every
// Kind cluster sits on the single "kind" network — so disconnecting everything on the network
// severs registries belonging to other, live clusters.
func DisconnectRegistriesByInfo(
	cmd *cobra.Command,
	networkName string,
	registries []dockerclient.RegistryInfo,
	cleanupDeps CleanupDependencies,
) error {
	if len(registries) == 0 || networkName == "" {
		return nil
	}

	return cleanupDeps.DockerInvoker(cmd, func(dockerClient dockerclient.Client) error {
		registryMgr, mgrErr := dockerclient.NewRegistryManager(dockerClient)
		if mgrErr != nil {
			return fmt.Errorf("failed to create registry manager: %w", mgrErr)
		}

		for _, reg := range registries {
			// Best-effort per registry: one that is already gone or never connected must not
			// stop the rest from being disconnected before the network is destroyed.
			_ = registryMgr.DisconnectFromNetwork(cmd.Context(), reg.Name, networkName)
		}

		return nil
	})
}

// DisconnectRegistriesFromNetwork disconnects all registries from a network.
// This is used for Talos which needs registries disconnected BEFORE cluster deletion.
func DisconnectRegistriesFromNetwork(
	cmd *cobra.Command,
	networkName string,
	cleanupDeps CleanupDependencies,
) error {
	return cleanupDeps.DockerInvoker(cmd, func(dockerClient dockerclient.Client) error {
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
