package registrystage

import (
	"context"
	"fmt"
	"io"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	dockerclient "github.com/devantler-tech/ksail/v5/pkg/client/docker"
	talosconfigmanager "github.com/devantler-tech/ksail/v5/pkg/io/config-manager/talos"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/registry"
	"github.com/devantler-tech/ksail/v5/pkg/utils/notify"
	"github.com/docker/docker/client"
)

// TalosRegistryAction returns the action function for Talos registry creation.
func TalosRegistryAction(ctx *Context) func(context.Context, client.APIClient) error {
	return func(execCtx context.Context, dockerClient client.APIClient) error {
		return runTalosRegistryAction(execCtx, ctx, dockerClient)
	}
}

// TalosNetworkAction returns the action function for Talos network creation.
func TalosNetworkAction(ctx *Context) func(context.Context, client.APIClient) error {
	return func(execCtx context.Context, dockerClient client.APIClient) error {
		return runTalosNetworkAction(execCtx, ctx, dockerClient)
	}
}

// TalosConnectAction returns the action function for Talos registry connection.
func TalosConnectAction(ctx *Context) func(context.Context, client.APIClient) error {
	return func(execCtx context.Context, dockerClient client.APIClient) error {
		return runTalosConnectAction(execCtx, ctx, dockerClient)
	}
}

// TalosPostClusterConnectAction returns the action function for post-cluster registry configuration.
// For Talos, this is a no-op since registry mirrors are configured via machine config before boot.
func TalosPostClusterConnectAction(_ *Context) func(context.Context, client.APIClient) error {
	return func(_ context.Context, _ client.APIClient) error {
		return nil
	}
}

// runTalosRegistryAction creates and configures registry containers.
func runTalosRegistryAction(
	execCtx context.Context,
	ctx *Context,
	dockerAPIClient client.APIClient,
) error {
	if len(ctx.MirrorSpecs) == 0 {
		return nil
	}

	clusterName := ResolveTalosClusterName(ctx.TalosConfig)
	writer := ctx.Cmd.OutOrStdout()

	// Build registry infos from mirror specs
	upstreams := registry.BuildUpstreamLookup(ctx.MirrorSpecs)
	registryInfos := registry.BuildRegistryInfosFromSpecs(
		ctx.MirrorSpecs,
		upstreams,
		nil,
		clusterName,
	)

	if len(registryInfos) == 0 {
		return nil
	}

	// Create registry manager and setup containers
	registryMgr, err := dockerclient.NewRegistryManager(dockerAPIClient)
	if err != nil {
		return fmt.Errorf("failed to create registry manager: %w", err)
	}

	err = registry.SetupRegistries(
		execCtx, registryMgr, registryInfos, clusterName, "", writer,
	)
	if err != nil {
		return fmt.Errorf("failed to setup talos registries: %w", err)
	}

	// Build registry IPs map for health check (empty IPs since we don't have network yet)
	registryIPs := make(map[string]string, len(registryInfos))
	for _, info := range registryInfos {
		registryIPs[info.Name] = ""
	}

	return waitForTalosRegistries(execCtx, registryMgr, registryIPs, writer)
}

// runTalosNetworkAction creates the Docker network for Talos.
func runTalosNetworkAction(
	execCtx context.Context,
	ctx *Context,
	dockerClient client.APIClient,
) error {
	if len(ctx.MirrorSpecs) == 0 {
		return nil
	}

	clusterName := ResolveTalosClusterName(ctx.TalosConfig)
	networkName := clusterName // Talos uses cluster name as network name
	networkCIDR := ResolveTalosNetworkCIDR(ctx.TalosConfig)
	writer := ctx.Cmd.OutOrStdout()

	return EnsureDockerNetworkExists(execCtx, dockerClient, networkName, networkCIDR, writer)
}

// runTalosConnectAction connects registries to the Docker network with static IPs.
func runTalosConnectAction(
	execCtx context.Context,
	ctx *Context,
	dockerAPIClient client.APIClient,
) error {
	if len(ctx.MirrorSpecs) == 0 {
		return nil
	}

	clusterName := ResolveTalosClusterName(ctx.TalosConfig)
	networkName := clusterName
	networkCIDR := ResolveTalosNetworkCIDR(ctx.TalosConfig)
	writer := ctx.Cmd.OutOrStdout()

	// Build registry infos from mirror specs
	upstreams := registry.BuildUpstreamLookup(ctx.MirrorSpecs)
	registryInfos := registry.BuildRegistryInfosFromSpecs(
		ctx.MirrorSpecs,
		upstreams,
		nil,
		clusterName,
	)

	if len(registryInfos) == 0 {
		return nil
	}

	// Connect registries to the network with static IPs
	_, err := registry.ConnectRegistriesToNetworkWithStaticIPs(
		execCtx, dockerAPIClient, registryInfos, networkName, networkCIDR, writer,
	)
	if err != nil {
		return fmt.Errorf("failed to connect talos registries to network: %w", err)
	}

	return nil
}

// ResolveTalosClusterName extracts the cluster name from Talos config or returns the default.
func ResolveTalosClusterName(talosConfig *talosconfigmanager.Configs) string {
	if talosConfig != nil && talosConfig.Name != "" {
		return talosConfig.Name
	}

	return talosconfigmanager.DefaultClusterName
}

// ResolveTalosNetworkCIDR returns the Docker network CIDR for Talos.
// This is always DefaultNetworkCIDR (10.5.0.0/24) - NOT the pod CIDR from cluster config.
// The Talos SDK uses this CIDR for the Docker bridge network that nodes connect to.
func ResolveTalosNetworkCIDR(_ *talosconfigmanager.Configs) string {
	return talosconfigmanager.DefaultNetworkCIDR
}

// waitForTalosRegistries waits for registries to become ready.
func waitForTalosRegistries(
	ctx context.Context,
	registryMgr *dockerclient.RegistryManager,
	registryIPs map[string]string,
	writer io.Writer,
) error {
	if len(registryIPs) == 0 {
		return nil
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.ActivityType,
		Content: "waiting for registries to become ready",
		Writer:  writer,
	})

	err := registryMgr.WaitForRegistriesReady(ctx, registryIPs)
	if err != nil {
		return fmt.Errorf("failed waiting for registries to become ready: %w", err)
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.SuccessType,
		Content: "all registries are ready",
		Writer:  writer,
	})

	return nil
}

// PrepareTalosConfigWithMirrors prepares the Talos config by setting up mirror registries.
// Returns true if mirror configuration is needed, false otherwise.
func PrepareTalosConfigWithMirrors(
	clusterCfg *v1alpha1.Cluster,
	talosConfig *talosconfigmanager.Configs,
	mirrorSpecs []registry.MirrorSpec,
	clusterName string,
) bool {
	if clusterCfg.Spec.Cluster.Distribution != v1alpha1.DistributionTalos {
		return false
	}

	if len(mirrorSpecs) == 0 {
		return false
	}

	// Apply mirror registries to the Talos config.
	// This enables --mirror-registry CLI flags to work for both:
	// 1. Clusters created solely from CLI with no declarative config
	// 2. Clusters with declarative config where additional mirrors are added via CLI
	if talosConfig != nil {
		mirrors := make([]talosconfigmanager.MirrorRegistry, 0, len(mirrorSpecs))
		for _, spec := range mirrorSpecs {
			// Use cluster name prefix for container name to avoid Docker DNS collisions
			// e.g., for cluster "talos-default", ghcr.io becomes "talos-default-ghcr.io"
			containerName := registry.BuildRegistryName(clusterName, spec.Host)
			mirrors = append(mirrors, talosconfigmanager.MirrorRegistry{
				Host:      spec.Host,
				Endpoints: []string{"http://" + containerName + ":5000"},
			})
		}

		// Apply mirrors to the Talos config - this merges with any existing mirrors
		_ = talosConfig.ApplyMirrorRegistries(mirrors)
	}

	return true
}
