package mirrorregistry

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

// resolveTalosRegistries resolves the cluster name and builds registry infos from the context.
// Returns empty slice if no mirror specs are provided.
// The usedPorts parameter allows avoiding port conflicts with existing containers.
func resolveTalosRegistries(ctx *Context, usedPorts map[int]struct{}) (string, []registry.Info) {
	clusterName := talosconfigmanager.ResolveClusterName(ctx.ClusterCfg, ctx.TalosConfig)
	registryInfos := buildTalosRegistryInfos(ctx.MirrorSpecs, clusterName, usedPorts)

	return clusterName, registryInfos
}

// runTalosRegistryAction creates and configures registry containers.
func runTalosRegistryAction(
	execCtx context.Context,
	ctx *Context,
	dockerAPIClient client.APIClient,
) error {
	// Create registry manager first to get used ports
	registryMgr, err := dockerclient.NewRegistryManager(dockerAPIClient)
	if err != nil {
		return fmt.Errorf("failed to create registry manager: %w", err)
	}

	// Get used ports from running containers to avoid conflicts
	usedPorts, err := registryMgr.GetUsedHostPorts(execCtx)
	if err != nil {
		return fmt.Errorf("failed to get used ports: %w", err)
	}

	clusterName, registryInfos := resolveTalosRegistries(ctx, usedPorts)

	if len(registryInfos) == 0 {
		return nil
	}

	writer := ctx.Cmd.OutOrStdout()

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

	clusterName := talosconfigmanager.ResolveClusterName(ctx.ClusterCfg, ctx.TalosConfig)
	networkName := clusterName // Talos uses cluster name as network name
	// Use DefaultNetworkCIDR (10.5.0.0/24) for the Docker bridge network.
	// This is the CIDR the Talos SDK uses for the Docker bridge network, NOT the pod CIDR.
	networkCIDR := talosconfigmanager.DefaultNetworkCIDR
	writer := ctx.Cmd.OutOrStdout()

	return EnsureDockerNetworkExists(execCtx, dockerClient, networkName, networkCIDR, writer)
}

// runTalosConnectAction connects registries to the Docker network with static IPs.
func runTalosConnectAction(
	execCtx context.Context,
	ctx *Context,
	dockerAPIClient client.APIClient,
) error {
	// Create registry manager to get used ports
	registryMgr, err := dockerclient.NewRegistryManager(dockerAPIClient)
	if err != nil {
		return fmt.Errorf("failed to create registry manager: %w", err)
	}

	// Get used ports from running containers (for consistent registry info building)
	usedPorts, err := registryMgr.GetUsedHostPorts(execCtx)
	if err != nil {
		return fmt.Errorf("failed to get used ports: %w", err)
	}

	clusterName, registryInfos := resolveTalosRegistries(ctx, usedPorts)

	if len(registryInfos) == 0 {
		return nil
	}

	networkName := clusterName
	networkCIDR := talosconfigmanager.DefaultNetworkCIDR
	writer := ctx.Cmd.OutOrStdout()

	// Connect registries to the network with static IPs
	_, err = registry.ConnectRegistriesToNetworkWithStaticIPs(
		execCtx, dockerAPIClient, registryInfos, networkName, networkCIDR, writer,
	)
	if err != nil {
		return fmt.Errorf("failed to connect talos registries to network: %w", err)
	}

	return nil
}

// buildTalosRegistryInfos builds registry infos from mirror specs for Talos.
// Returns nil if no mirror specs are provided.
// The usedPorts parameter allows avoiding port conflicts with existing containers.
func buildTalosRegistryInfos(
	mirrorSpecs []registry.MirrorSpec,
	clusterName string,
	usedPorts map[int]struct{},
) []registry.Info {
	if len(mirrorSpecs) == 0 {
		return nil
	}

	upstreams := registry.BuildUpstreamLookup(mirrorSpecs)

	return registry.BuildRegistryInfosFromSpecs(
		mirrorSpecs,
		upstreams,
		usedPorts,
		clusterName,
	)
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
