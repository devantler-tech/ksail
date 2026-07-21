package mirrorregistry

import (
	"fmt"

	dockerclient "github.com/devantler-tech/ksail/v7/pkg/client/docker"
	"github.com/spf13/cobra"
)

// DisconnectRegistriesByInfo detaches the given registries from a network.
//
// Teardown uses this rather than detaching everything on the network, because a network name does
// not identify a single cluster. Kind's "kind" network is shared by every Kind cluster, and the
// generated names collide across distributions as well: cluster names may contain hyphens, so a
// K3d cluster "foo" and a Talos cluster "k3d-foo" both resolve to "k3d-foo". Detaching by network
// alone would sever a live bystander's registry mirrors.
//
// Best-effort per registry: one that is already gone, or was never connected, must not stop the
// rest from being detached before the network is destroyed.
func DisconnectRegistriesByInfo(
	cmd *cobra.Command,
	networkName string,
	registries []dockerclient.RegistryInfo,
	cleanupDeps CleanupDependencies,
) error {
	if networkName == "" || len(registries) == 0 {
		return nil
	}

	return cleanupDeps.DockerInvoker(cmd, func(dockerClient dockerclient.Client) error {
		registryMgr, mgrErr := dockerclient.NewRegistryManager(dockerClient)
		if mgrErr != nil {
			return fmt.Errorf("failed to create registry manager: %w", mgrErr)
		}

		for _, reg := range registries {
			_ = registryMgr.DisconnectFromNetwork(cmd.Context(), reg.Name, networkName)
		}

		return nil
	})
}
