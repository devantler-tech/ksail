package registrystage

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	k3dconfigmanager "github.com/devantler-tech/ksail/v5/pkg/io/config-manager/k3d"
	k3dprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/k3d"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/registry"
	"github.com/docker/docker/client"
	"github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
)

// K3dRegistryAction is the function signature for K3d registry actions.
type K3dRegistryAction func(
	context.Context,
	*v1alpha5.SimpleConfig,
	string,
	client.APIClient,
	io.Writer,
) error

// K3dMirrorAction returns the action function for K3d mirror registry setup.
func K3dMirrorAction(ctx *Context) func(context.Context, client.APIClient) error {
	return func(execCtx context.Context, dockerClient client.APIClient) error {
		// Setup registries first
		err := runK3DRegistryAction(
			execCtx,
			ctx,
			dockerClient,
			"setup k3d registries",
			k3dprovisioner.SetupRegistries,
		)
		if err != nil {
			return err
		}

		// Pre-create Docker network and connect registries before cluster creation.
		// This ensures registries are reachable via Docker DNS when K3d nodes pull images.
		clusterName := k3dconfigmanager.ResolveClusterName(ctx.ClusterCfg, ctx.K3dConfig)
		networkName := "k3d-" + clusterName
		writer := ctx.Cmd.OutOrStdout()

		// For K3d, we don't need to specify a CIDR as K3d manages its own network settings.
		errNetwork := EnsureDockerNetworkExists(
			execCtx,
			dockerClient,
			networkName,
			"",
			writer,
		)
		if errNetwork != nil {
			return fmt.Errorf("failed to create k3d network: %w", errNetwork)
		}

		// Connect registries to network
		errConnect := runK3DRegistryAction(
			execCtx,
			ctx,
			dockerClient,
			"connect k3d registries to network",
			k3dprovisioner.ConnectRegistriesToNetwork,
		)
		if errConnect != nil {
			return errConnect
		}

		// Wait for registries to become ready before proceeding.
		// This prevents intermittent image pull failures due to race conditions.
		registryInfos := k3dprovisioner.ExtractRegistriesFromConfigForTesting(ctx.K3dConfig)

		return WaitForRegistriesReady(execCtx, dockerClient, registryInfos, writer)
	}
}

// K3dConnectAction returns the action function for K3d registry connection.
func K3dConnectAction(_ *Context) func(context.Context, client.APIClient) error {
	// Registries are already connected to the network in the mirror action.
	// No additional work needed after cluster creation.
	return func(_ context.Context, _ client.APIClient) error {
		return nil
	}
}

func runK3DRegistryAction(
	execCtx context.Context,
	ctx *Context,
	dockerClient client.APIClient,
	description string,
	action K3dRegistryAction,
) error {
	if action == nil {
		return nil
	}

	targetName := k3dconfigmanager.ResolveClusterName(ctx.ClusterCfg, ctx.K3dConfig)
	writer := ctx.Cmd.OutOrStdout()

	err := action(execCtx, ctx.K3dConfig, targetName, dockerClient, writer)
	if err != nil {
		return fmt.Errorf("failed to %s: %w", description, err)
	}

	return nil
}

// PrepareK3dConfigWithMirrors prepares the K3d config by setting up mirror registries.
// Returns true if mirror configuration is needed, false otherwise.
func PrepareK3dConfigWithMirrors(
	clusterCfg *v1alpha1.Cluster,
	k3dConfig *v1alpha5.SimpleConfig,
	mirrorSpecs []registry.MirrorSpec,
) bool {
	if clusterCfg.Spec.Cluster.Distribution != v1alpha1.DistributionK3d || k3dConfig == nil {
		return false
	}

	original := k3dConfig.Registries.Config

	hostEndpoints := k3dconfigmanager.ParseRegistryConfig(original)

	updatedMap, _ := registry.BuildHostEndpointMap(mirrorSpecs, "", hostEndpoints)
	if len(updatedMap) == 0 {
		return false
	}

	rendered := registry.RenderK3dMirrorConfig(updatedMap)

	if strings.TrimSpace(rendered) == strings.TrimSpace(original) {
		return strings.TrimSpace(original) != ""
	}

	k3dConfig.Registries.Config = rendered

	return true
}
