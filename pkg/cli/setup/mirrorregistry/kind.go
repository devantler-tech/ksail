package mirrorregistry

import (
	"context"
	"fmt"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	kindconfigmanager "github.com/devantler-tech/ksail/v5/pkg/io/config-manager/kind"
	kindprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/kind"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/registry"
	"github.com/docker/docker/client"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

// KindRegistryAction returns the action function for Kind registry creation.
func KindRegistryAction(ctx *Context) func(context.Context, client.APIClient) error {
	return func(execCtx context.Context, dockerClient client.APIClient) error {
		return runKindRegistryAction(execCtx, ctx, dockerClient)
	}
}

// KindNetworkAction returns the action function for Kind network creation.
func KindNetworkAction(ctx *Context) func(context.Context, client.APIClient) error {
	return func(execCtx context.Context, dockerClient client.APIClient) error {
		return runKindNetworkAction(execCtx, ctx, dockerClient)
	}
}

// KindConnectAction returns the action function for Kind registry connection.
func KindConnectAction(ctx *Context) func(context.Context, client.APIClient) error {
	return func(execCtx context.Context, dockerClient client.APIClient) error {
		return runKindConnectAction(execCtx, ctx, dockerClient)
	}
}

// KindPostClusterConnectAction returns the action function for post-cluster registry configuration.
func KindPostClusterConnectAction(ctx *Context) func(context.Context, client.APIClient) error {
	return func(execCtx context.Context, dockerClient client.APIClient) error {
		return runKindPostClusterConnectAction(execCtx, ctx, dockerClient)
	}
}

// runKindRegistryAction creates and configures registry containers (without network attachment).
func runKindRegistryAction(
	execCtx context.Context,
	ctx *Context,
	dockerClient client.APIClient,
) error {
	writer := ctx.Cmd.OutOrStdout()
	clusterName := kindconfigmanager.ResolveClusterName(ctx.ClusterCfg, ctx.KindConfig)

	// Setup registry containers (without network attachment yet)
	err := kindprovisioner.SetupRegistries(
		execCtx,
		ctx.KindConfig,
		clusterName,
		dockerClient,
		ctx.MirrorSpecs,
		writer,
	)
	if err != nil {
		return fmt.Errorf("failed to setup kind registries: %w", err)
	}

	// Wait for registries to become ready BEFORE connecting to network.
	// This is critical: registry v3 panics if it can't reach the upstream during startup.
	// If we connect to network first, Docker DNS will resolve the upstream hostname
	// (e.g., ghcr.io) to the container's own IP (since container is named ghcr.io),
	// causing the registry to connect to itself and fail.
	registryInfos := registry.BuildRegistryInfosFromSpecs(ctx.MirrorSpecs, nil, nil, clusterName)

	return WaitForRegistriesReady(execCtx, dockerClient, registryInfos, writer)
}

// runKindNetworkAction creates the Docker network for Kind.
func runKindNetworkAction(
	execCtx context.Context,
	ctx *Context,
	dockerClient client.APIClient,
) error {
	writer := ctx.Cmd.OutOrStdout()

	// Create the Docker network. For Kind, we don't need to specify a CIDR
	// as Kind manages its own network settings.
	return EnsureDockerNetworkExists(
		execCtx,
		dockerClient,
		kindconfigmanager.DefaultNetworkName,
		"",
		writer,
	)
}

// runKindConnectAction connects registries to the Docker network.
func runKindConnectAction(
	execCtx context.Context,
	ctx *Context,
	dockerClient client.APIClient,
) error {
	writer := ctx.Cmd.OutOrStdout()
	clusterName := kindconfigmanager.ResolveClusterName(ctx.ClusterCfg, ctx.KindConfig)

	// Connect registries to the network for Docker DNS resolution by Kind nodes.
	// The registries are already running and healthy at this point.
	err := kindprovisioner.ConnectRegistriesToNetwork(
		execCtx,
		ctx.MirrorSpecs,
		clusterName,
		dockerClient,
		writer,
	)
	if err != nil {
		return fmt.Errorf("connect registries to network: %w", err)
	}

	return nil
}

// runKindPostClusterConnectAction configures containerd inside Kind nodes to use registry mirrors.
func runKindPostClusterConnectAction(
	execCtx context.Context,
	ctx *Context,
	dockerClient client.APIClient,
) error {
	// This function only configures containerd inside Kind nodes to use the registry mirrors.
	// This injects hosts.toml files directly into the running nodes.
	err := kindprovisioner.ConfigureContainerdRegistryMirrors(
		execCtx,
		ctx.KindConfig,
		ctx.MirrorSpecs,
		dockerClient,
		ctx.Cmd.OutOrStdout(),
	)
	if err != nil {
		return fmt.Errorf("failed to configure containerd registry mirrors: %w", err)
	}

	return nil
}

// PrepareKindConfigWithMirrors prepares the Kind config by setting up hosts directory for mirrors.
// Returns true if mirror configuration is needed, false otherwise.
// This uses the modern hosts directory pattern instead of deprecated ContainerdConfigPatches.
// Note: mirrorSpecs should be the pre-computed merged specs from RunStage.
func PrepareKindConfigWithMirrors(
	clusterCfg *v1alpha1.Cluster,
	kindConfig *v1alpha4.Cluster,
	mirrorSpecs []registry.MirrorSpec,
) bool {
	// Only for Kind distribution
	if clusterCfg.Spec.Cluster.Distribution != v1alpha1.DistributionVanilla || kindConfig == nil {
		return false
	}

	// If we have any mirror specs, configuration is needed
	return len(mirrorSpecs) > 0
}

// GetKindMirrorsDir returns the configured Kind mirrors directory or the default.
//
// Deprecated: Use kindconfigmanager.ResolveMirrorsDir instead.
func GetKindMirrorsDir(clusterCfg *v1alpha1.Cluster) string {
	return kindconfigmanager.ResolveMirrorsDir(clusterCfg)
}
