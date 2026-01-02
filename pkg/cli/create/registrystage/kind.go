package registrystage

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

// KindMirrorAction returns the action function for Kind mirror registry setup.
func KindMirrorAction(ctx *Context) func(context.Context, client.APIClient) error {
	return func(execCtx context.Context, dockerClient client.APIClient) error {
		return runKindMirrorAction(execCtx, ctx, dockerClient)
	}
}

// KindConnectAction returns the action function for Kind registry connection.
func KindConnectAction(ctx *Context) func(context.Context, client.APIClient) error {
	return func(execCtx context.Context, dockerClient client.APIClient) error {
		return runKindConnectAction(execCtx, ctx, dockerClient)
	}
}

func runKindMirrorAction(
	execCtx context.Context,
	ctx *Context,
	dockerClient client.APIClient,
) error {
	writer := ctx.Cmd.OutOrStdout()
	clusterName := ctx.KindConfig.Name

	// Kind always uses a network named "kind"
	networkName := "kind"

	// Pre-create the Docker network so registries can be connected before Kind nodes start.
	// This ensures registry containers are reachable via Docker DNS when nodes pull images.
	// For Kind, we don't need to specify a CIDR as Kind manages its own network settings.
	err := EnsureDockerNetworkExists(execCtx, dockerClient, networkName, "", writer)
	if err != nil {
		return fmt.Errorf("failed to create docker network: %w", err)
	}

	// Setup registry containers
	err = kindprovisioner.SetupRegistries(
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

	// Connect registries to the network immediately for Docker DNS resolution
	err = kindprovisioner.ConnectRegistriesToNetwork(
		execCtx,
		ctx.MirrorSpecs,
		dockerClient,
		writer,
	)
	if err != nil {
		return fmt.Errorf("failed to connect kind registries to network: %w", err)
	}

	// Wait for registries to become ready before proceeding.
	// This prevents intermittent image pull failures due to race conditions.
	registryInfos := registry.BuildRegistryInfosFromSpecs(ctx.MirrorSpecs, nil, nil, "")

	return WaitForRegistriesReady(execCtx, dockerClient, registryInfos, writer)
}

func runKindConnectAction(
	execCtx context.Context,
	ctx *Context,
	dockerClient client.APIClient,
) error {
	// Registries are already connected to the network in runKindMirrorAction.
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
