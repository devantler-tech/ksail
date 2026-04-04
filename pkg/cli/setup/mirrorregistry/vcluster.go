package mirrorregistry

import (
	"context"
	"fmt"
	"strings"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	clusterprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster"
	vclusterprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/vcluster"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/registry"
	"github.com/docker/docker/client"
)

// vclusterDefaultClusterName is the default cluster name for VCluster.
const vclusterDefaultClusterName = "vcluster-default"

// VClusterRegistryAction returns the action function for VCluster registry creation.
func VClusterRegistryAction(ctx *Context) func(context.Context, client.APIClient) error {
	return func(execCtx context.Context, dockerClient client.APIClient) error {
		return runVClusterRegistryAction(execCtx, ctx, dockerClient)
	}
}

// vclusterNetworkPrefix is the Docker network name prefix used by VCluster.
const vclusterNetworkPrefix = "vcluster."

// VClusterNetworkAction returns the action function for VCluster network creation.
// Pre-creates the Docker network so mirror registries can be connected before
// cluster creation. The VCluster SDK reuses an existing network with this name.
func VClusterNetworkAction(ctx *Context) func(context.Context, client.APIClient) error {
	return func(execCtx context.Context, dockerClient client.APIClient) error {
		clusterName := resolveVClusterClusterName(ctx.VClusterConfig)
		networkName := vclusterNetworkPrefix + clusterName
		writer := ctx.Cmd.OutOrStdout()

		return EnsureDockerNetworkExists(execCtx, dockerClient, networkName, "", writer)
	}
}

// VClusterConnectAction returns the action function for VCluster registry connection.
func VClusterConnectAction(ctx *Context) func(context.Context, client.APIClient) error {
	return func(execCtx context.Context, dockerClient client.APIClient) error {
		return runVClusterConnectAction(execCtx, ctx, dockerClient)
	}
}

// VClusterPostClusterConnectAction returns the action function for post-cluster
// registry configuration. This injects hosts.toml files into VCluster nodes via
// docker exec, the same approach used by Kind.
func VClusterPostClusterConnectAction(ctx *Context) func(context.Context, client.APIClient) error {
	return func(execCtx context.Context, dockerClient client.APIClient) error {
		return runVClusterPostClusterConnectAction(execCtx, ctx, dockerClient)
	}
}

// resolveVClusterClusterName determines the VCluster cluster name from config.
// It follows the same resolution logic as localregistry/resolve.go.
func resolveVClusterClusterName(vclusterConfig *clusterprovisioner.VClusterConfig) string {
	if vclusterConfig != nil {
		if name := strings.TrimSpace(vclusterConfig.GetClusterName()); name != "" {
			return name
		}
	}

	return vclusterDefaultClusterName
}

// runVClusterRegistryAction creates and configures registry containers (without network attachment).
func runVClusterRegistryAction(
	execCtx context.Context,
	ctx *Context,
	dockerClient client.APIClient,
) error {
	writer := ctx.Cmd.OutOrStdout()
	clusterName := resolveVClusterClusterName(ctx.VClusterConfig)

	err := vclusterprovisioner.SetupRegistries(
		execCtx, clusterName, dockerClient, ctx.MirrorSpecs, writer,
	)
	if err != nil {
		return fmt.Errorf("failed to setup vcluster registries: %w", err)
	}

	// Wait for registries to become ready before connecting to network.
	registryInfos := registry.BuildRegistryInfosFromSpecs(ctx.MirrorSpecs, nil, nil, clusterName)

	return WaitForRegistriesReady(execCtx, dockerClient, registryInfos, writer)
}

// runVClusterConnectAction connects registries to the VCluster Docker network.
func runVClusterConnectAction(
	execCtx context.Context,
	ctx *Context,
	dockerClient client.APIClient,
) error {
	writer := ctx.Cmd.OutOrStdout()
	clusterName := resolveVClusterClusterName(ctx.VClusterConfig)

	err := vclusterprovisioner.ConnectRegistriesToNetwork(
		execCtx, ctx.MirrorSpecs, clusterName, dockerClient, writer,
	)
	if err != nil {
		return fmt.Errorf("connect registries to vcluster network: %w", err)
	}

	return nil
}

// runVClusterPostClusterConnectAction configures containerd inside VCluster nodes
// to use registry mirrors by injecting hosts.toml files.
func runVClusterPostClusterConnectAction(
	execCtx context.Context,
	ctx *Context,
	dockerClient client.APIClient,
) error {
	clusterName := resolveVClusterClusterName(ctx.VClusterConfig)

	err := vclusterprovisioner.ConfigureContainerdRegistryMirrors(
		execCtx, clusterName, ctx.MirrorSpecs, dockerClient, ctx.Cmd.OutOrStdout(),
	)
	if err != nil {
		return fmt.Errorf("failed to configure containerd registry mirrors: %w", err)
	}

	return nil
}

// PrepareVClusterConfigWithMirrors checks if VCluster mirror configuration is needed.
// Returns true if mirror specs are available, false otherwise.
func PrepareVClusterConfigWithMirrors(
	clusterCfg *v1alpha1.Cluster,
	mirrorSpecs []registry.MirrorSpec,
) bool {
	if clusterCfg.Spec.Cluster.Distribution != v1alpha1.DistributionVCluster {
		return false
	}

	return len(mirrorSpecs) > 0
}
