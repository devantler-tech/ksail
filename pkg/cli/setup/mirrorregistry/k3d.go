package mirrorregistry

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

// k3dRegistryActionFn is the function signature for K3d registry actions.
type k3dRegistryActionFn func(
	context.Context,
	*v1alpha5.SimpleConfig,
	string,
	client.APIClient,
	io.Writer,
) error

// K3dRegistryAction returns the action function for K3d registry creation.
func K3dRegistryAction(ctx *Context) func(context.Context, client.APIClient) error {
	return func(execCtx context.Context, dockerClient client.APIClient) error {
		return runK3dRegistryAction(execCtx, ctx, dockerClient)
	}
}

// K3dNetworkAction returns the action function for K3d network creation.
func K3dNetworkAction(ctx *Context) func(context.Context, client.APIClient) error {
	return func(execCtx context.Context, dockerClient client.APIClient) error {
		return runK3dNetworkAction(execCtx, ctx, dockerClient)
	}
}

// K3dConnectAction returns the action function for K3d registry connection.
func K3dConnectAction(ctx *Context) func(context.Context, client.APIClient) error {
	return func(execCtx context.Context, dockerClient client.APIClient) error {
		return runK3dConnectAction(execCtx, ctx, dockerClient)
	}
}

// K3dPostClusterConnectAction returns the action function for post-cluster registry configuration.
// For K3d, this is a no-op since registry mirrors are configured via k3d config before cluster creation.
func K3dPostClusterConnectAction(_ *Context) func(context.Context, client.APIClient) error {
	return func(_ context.Context, _ client.APIClient) error {
		return nil
	}
}

// runK3dRegistryAction creates and configures registry containers.
func runK3dRegistryAction(
	execCtx context.Context,
	ctx *Context,
	dockerClient client.APIClient,
) error {
	// Setup registries
	err := runK3DRegistrySetup(
		execCtx,
		ctx,
		dockerClient,
		"setup k3d registries",
		k3dprovisioner.SetupRegistries,
	)
	if err != nil {
		return err
	}

	// Wait for registries to become ready before network connection.
	registryInfos := k3dprovisioner.ExtractRegistriesFromConfigForTesting(ctx.K3dConfig)
	writer := ctx.Cmd.OutOrStdout()

	return WaitForRegistriesReady(execCtx, dockerClient, registryInfos, writer)
}

// runK3dNetworkAction creates the Docker network for K3d.
func runK3dNetworkAction(
	execCtx context.Context,
	ctx *Context,
	dockerClient client.APIClient,
) error {
	clusterName := k3dconfigmanager.ResolveClusterName(ctx.ClusterCfg, ctx.K3dConfig)
	networkName := "k3d-" + clusterName
	writer := ctx.Cmd.OutOrStdout()

	// For K3d, we don't need to specify a CIDR as K3d manages its own network settings.
	return EnsureDockerNetworkExists(execCtx, dockerClient, networkName, "", writer)
}

// runK3dConnectAction connects registries to the Docker network.
func runK3dConnectAction(
	execCtx context.Context,
	ctx *Context,
	dockerClient client.APIClient,
) error {
	return runK3DRegistrySetup(
		execCtx,
		ctx,
		dockerClient,
		"connect k3d registries to network",
		k3dprovisioner.ConnectRegistriesToNetwork,
	)
}

func runK3DRegistrySetup(
	execCtx context.Context,
	ctx *Context,
	dockerClient client.APIClient,
	description string,
	action k3dRegistryActionFn,
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
