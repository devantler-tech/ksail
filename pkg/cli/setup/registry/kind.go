package registry

import (
	"context"
	"fmt"
	"os"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	ksailconfigmanager "github.com/devantler-tech/ksail/v5/pkg/io/config-manager/ksail"
	"github.com/devantler-tech/ksail/v5/pkg/io/scaffolder"
	kindprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/kind"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/registry"
	"github.com/devantler-tech/ksail/v5/pkg/utils/notify"
	"github.com/docker/docker/client"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

// Kind network name constant.
const kindNetworkName = "kind"

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
	clusterName := ctx.KindConfig.Name

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
	registryInfos := registry.BuildRegistryInfosFromSpecs(ctx.MirrorSpecs, nil, nil, "")

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
	return EnsureDockerNetworkExists(execCtx, dockerClient, kindNetworkName, "", writer)
}

// runKindConnectAction connects registries to the Docker network.
func runKindConnectAction(
	execCtx context.Context,
	ctx *Context,
	dockerClient client.APIClient,
) error {
	writer := ctx.Cmd.OutOrStdout()

	// Connect registries to the network for Docker DNS resolution by Kind nodes.
	// The registries are already running and healthy at this point.
	err := kindprovisioner.ConnectRegistriesToNetwork(
		execCtx,
		ctx.MirrorSpecs,
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
func PrepareKindConfigWithMirrors(
	clusterCfg *v1alpha1.Cluster,
	cfgManager *ksailconfigmanager.ConfigManager,
	kindConfig *v1alpha4.Cluster,
) bool {
	// Only for Kind distribution
	if clusterCfg.Spec.Cluster.Distribution != v1alpha1.DistributionKind || kindConfig == nil {
		return false
	}

	// Check for --mirror-registry flag
	mirrorRegistries := cfgManager.Viper.GetStringSlice("mirror-registry")

	// Also check for existing hosts.toml files
	existingSpecs, err := registry.ReadExistingHostsToml(GetKindMirrorsDir(clusterCfg))
	if err != nil {
		// Log error but don't fail - missing configuration is acceptable
		notify.WriteMessage(notify.Message{
			Type:    notify.WarningType,
			Content: "failed to read existing hosts configuration: %v",
			Args:    []any{err},
			Writer:  os.Stderr,
		})
	}

	// If we have either flag specs or existing specs, configuration is needed
	if len(mirrorRegistries) > 0 || len(existingSpecs) > 0 {
		return true
	}

	return false
}

// GetKindMirrorsDir returns the configured Kind mirrors directory or the default.
func GetKindMirrorsDir(clusterCfg *v1alpha1.Cluster) string {
	if clusterCfg != nil && clusterCfg.Spec.Cluster.Kind.MirrorsDir != "" {
		return clusterCfg.Spec.Cluster.Kind.MirrorsDir
	}

	return scaffolder.DefaultKindMirrorsDir
}
