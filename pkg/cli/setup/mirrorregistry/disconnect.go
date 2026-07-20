package mirrorregistry

import (
	"fmt"

	dockerclient "github.com/devantler-tech/ksail/v7/pkg/client/docker"
	"github.com/spf13/cobra"
)

// DisconnectRegistriesByInfo disconnects only the given registries from a network.
//
// Disconnecting everything attached to a network is not safe here: some networks are SHARED
// between clusters — every Kind cluster sits on the single "kind" network — so a network-wide
// disconnect severs registries belonging to other, live clusters. Callers therefore pass the
// registries they have already scoped to the cluster being torn down.
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
