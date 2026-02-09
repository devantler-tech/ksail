package mirrorregistry

import (
	"fmt"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/cli/lifecycle"
	"github.com/devantler-tech/ksail/v5/pkg/cli/setup/localregistry"
	dockerclient "github.com/devantler-tech/ksail/v5/pkg/client/docker"
	ksailconfigmanager "github.com/devantler-tech/ksail/v5/pkg/io/config-manager/ksail"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/registry"
	"github.com/devantler-tech/ksail/v5/pkg/notify"
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

// DisconnectMirrorRegistries disconnects mirror registries from the Talos network.
// This allows the network to be removed during cluster deletion without "active endpoints" errors.
func DisconnectMirrorRegistries(
	cmd *cobra.Command,
	cfgManager *ksailconfigmanager.ConfigManager,
	clusterName string,
	cleanupDeps CleanupDependencies,
	provider v1alpha1.Provider,
) error {
	// Collect mirror specs from Talos config
	mirrorSpecs, registryNames := CollectTalosMirrorSpecs(cmd, cfgManager, provider)

	// Talos uses the cluster name as the network name
	networkName := clusterName

	err := cleanupDeps.DockerInvoker(cmd, func(dockerAPIClient client.APIClient) error {
		registryMgr, mgrErr := dockerclient.NewRegistryManager(dockerAPIClient)
		if mgrErr != nil {
			return fmt.Errorf("failed to create registry manager: %w", mgrErr)
		}

		// If no registry names found from config (non-scaffolded cluster),
		// fall back to discovering and disconnecting all registries from the network
		if len(registryNames) == 0 {
			_, disconnectErr := registryMgr.DisconnectAllFromNetwork(cmd.Context(), networkName)
			if disconnectErr != nil {
				return fmt.Errorf(
					"failed to disconnect registries from network %s: %w",
					networkName,
					disconnectErr,
				)
			}

			return nil
		}

		// Build registry infos from mirror specs to get container names
		registryInfos := registry.BuildRegistryInfosFromSpecs(
			mirrorSpecs,
			nil,
			nil,
			clusterName,
		)

		// Disconnect each registry from the network
		for _, info := range registryInfos {
			disconnectErr := registryMgr.DisconnectFromNetwork(
				cmd.Context(),
				info.Name,
				networkName,
			)
			if disconnectErr != nil {
				return fmt.Errorf(
					"failed to disconnect registry %s from network: %w",
					info.Name,
					disconnectErr,
				)
			}
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("disconnect mirror registries: %w", err)
	}

	return nil
}

// DisconnectMirrorRegistriesWithWarning disconnects mirror registries from the network.
// This is used for Talos which needs registries disconnected BEFORE cluster deletion
// due to network dependencies, while actual container cleanup happens after deletion.
func DisconnectMirrorRegistriesWithWarning(
	cmd *cobra.Command,
	cfgManager *ksailconfigmanager.ConfigManager,
	clusterName string,
	cleanupDeps CleanupDependencies,
	provider v1alpha1.Provider,
) {
	err := DisconnectMirrorRegistries(cmd, cfgManager, clusterName, cleanupDeps, provider)
	if err != nil {
		notify.WriteMessage(notify.Message{
			Type:    notify.ErrorType,
			Content: fmt.Sprintf("failed to disconnect mirror registries: %v", err),
			Writer:  cmd.OutOrStdout(),
		})
	}
}

// DisconnectLocalRegistryWithWarning disconnects the local registry from the cluster network.
// This is used for Talos which needs registries disconnected BEFORE cluster deletion
// because the registry is connected to the cluster network.
func DisconnectLocalRegistryWithWarning(
	cmd *cobra.Command,
	cfgManager *ksailconfigmanager.ConfigManager,
	clusterCfg *v1alpha1.Cluster,
	deps lifecycle.Deps,
	clusterName string,
	cleanupDeps CleanupDependencies,
) {
	err := localregistry.Disconnect(
		cmd,
		cfgManager,
		clusterCfg,
		deps,
		clusterName,
		cleanupDeps.LocalRegistryDeps,
	)
	if err != nil {
		notify.WriteMessage(notify.Message{
			Type:    notify.ErrorType,
			Content: fmt.Sprintf("failed to disconnect local registry: %v", err),
			Writer:  cmd.OutOrStdout(),
		})
	}
}
