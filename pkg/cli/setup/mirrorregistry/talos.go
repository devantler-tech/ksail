package mirrorregistry

import (
	"context"
	"fmt"
	"io"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	dockerclient "github.com/devantler-tech/ksail/v7/pkg/client/docker"
	talosconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
	"github.com/devantler-tech/ksail/v7/pkg/notify"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/registry"
)

// resolveTalosRegistries resolves the cluster name and builds registry infos from the context.
// Returns empty slice if no mirror specs are provided.
// The usedPorts parameter allows avoiding port conflicts with existing containers.
func resolveTalosRegistries(ctx *Context, usedPorts map[int]struct{}) (string, []registry.Info) {
	clusterName := talosconfigmanager.ResolveClusterName(ctx.ClusterCfg, ctx.TalosConfig)
	registryInfos := buildTalosRegistryInfos(ctx.MirrorSpecs, clusterName, usedPorts)

	return clusterName, registryInfos
}

// resolveTalosBackendRegistries creates the registry backend and resolves the cluster's registry
// set from its currently-used ports — the setup shared by runTalosRegistryAction and
// runTalosConnectAction before they diverge on what to do with the result. The returned bool is
// false when there is nothing to configure (caller should return nil).
func resolveTalosBackendRegistries(
	execCtx context.Context,
	ctx *Context,
	dockerAPIClient dockerclient.Client,
) (registry.Backend, string, []registry.Info, bool, error) {
	// Create registry backend using the factory (allows test injection)
	backend, err := GetBackendFactory()(dockerAPIClient)
	if err != nil {
		return nil, "", nil, false, fmt.Errorf("failed to create registry backend: %w", err)
	}

	// Get used ports from running containers to avoid conflicts
	usedPorts, err := getUsedPorts(execCtx, backend)
	if err != nil {
		return nil, "", nil, false, fmt.Errorf("failed to get used ports: %w", err)
	}

	clusterName, registryInfos := resolveTalosRegistries(ctx, usedPorts)

	return backend, clusterName, registryInfos, len(registryInfos) > 0, nil
}

// runTalosRegistryAction creates and configures registry containers.
func runTalosRegistryAction(
	execCtx context.Context,
	ctx *Context,
	dockerAPIClient dockerclient.Client,
) error {
	backend, clusterName, registryInfos, hasRegistries, err := resolveTalosBackendRegistries(
		execCtx,
		ctx,
		dockerAPIClient,
	)
	if err != nil {
		return err
	}

	if !hasRegistries {
		return nil
	}

	writer := ctx.Cmd.OutOrStdout()

	err = registry.SetupRegistries(
		execCtx, backend, registryInfos, clusterName, "", writer,
	)
	if err != nil {
		return fmt.Errorf("failed to setup talos registries: %w", err)
	}

	// Build registry IPs map for health check (empty IPs since we don't have network yet)
	registryIPs := make(map[string]string, len(registryInfos))
	for _, info := range registryInfos {
		registryIPs[info.Name] = ""
	}

	return waitForTalosRegistries(execCtx, backend, registryIPs, writer)
}

// runTalosNetworkAction creates the Docker network for Talos.
func runTalosNetworkAction(
	execCtx context.Context,
	ctx *Context,
	dockerClient dockerclient.Client,
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
	dockerAPIClient dockerclient.Client,
) error {
	_, clusterName, registryInfos, hasRegistries, err := resolveTalosBackendRegistries(
		execCtx,
		ctx,
		dockerAPIClient,
	)
	if err != nil {
		return err
	}

	if !hasRegistries {
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

// PortGetter is an interface for getting used host ports.
// This is a subset of the Backend interface used for port collection.
type PortGetter interface {
	GetRegistryPort(ctx context.Context, name string) (int, error)
	ListRegistries(ctx context.Context) ([]string, error)
}

// getUsedPorts collects used ports from the backend if it supports GetUsedHostPorts.
// Returns empty map if backend doesn't support port collection or no ports are in use.
func getUsedPorts(ctx context.Context, backend registry.Backend) (map[int]struct{}, error) {
	// Try to get used ports if the backend supports it
	ports, err := registry.CollectExistingRegistryPorts(ctx, backend)
	if err != nil {
		return nil, fmt.Errorf("failed to collect existing registry ports: %w", err)
	}

	return ports, nil
}

// ReadinessChecker is an interface for checking registry readiness.
type ReadinessChecker interface {
	WaitForRegistriesReady(ctx context.Context, registryIPs map[string]string) error
}

// waitForTalosRegistries waits for registries to become ready.
func waitForTalosRegistries(
	ctx context.Context,
	backend registry.Backend,
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

	// Check if the backend supports readiness checking
	if checker, ok := backend.(ReadinessChecker); ok {
		err := checker.WaitForRegistriesReady(ctx, registryIPs)
		if err != nil {
			return fmt.Errorf("failed waiting for registries to become ready: %w", err)
		}
	}
	// If backend doesn't support readiness checking (e.g., mock), skip it

	notify.WriteMessage(notify.Message{
		Type:    notify.ActivityType,
		Content: "registries are ready",
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
		mirrors := registry.BuildTalosMirrorRegistries(mirrorSpecs, clusterName)

		// Apply mirrors to the Talos config - this merges with any existing mirrors
		_ = talosConfig.ApplyMirrorRegistries(mirrors)
	}

	return true
}
