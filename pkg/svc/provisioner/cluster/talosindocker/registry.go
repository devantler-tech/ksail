package talosindockerprovisioner

import (
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"

	dockerclient "github.com/devantler-tech/ksail/v5/pkg/client/docker"
	talosconfigmanager "github.com/devantler-tech/ksail/v5/pkg/io/config-manager/talos"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/registry"
	"github.com/docker/docker/client"
)

// SetupRegistries creates mirror registries based on mirror specifications.
// Registries are created without network attachment first, as the TalosInDocker network
// doesn't exist until after the cluster is created. mirrorSpecs should contain the
// user-supplied mirror definitions so upstream URLs can be preserved when creating
// local proxy registries.
//
// Unlike Kind which uses hosts.toml files, TalosInDocker configures registry mirrors
// via machine configuration patches. The registry containers are still the same.
func SetupRegistries(
	ctx context.Context,
	talosConfig *talosconfigmanager.Configs,
	clusterName string,
	dockerClient client.APIClient,
	mirrorSpecs []registry.MirrorSpec,
	writer io.Writer,
) error {
	registryMgr, registriesInfo, err := prepareTalosRegistryManager(ctx, mirrorSpecs, dockerClient)
	if err != nil {
		return fmt.Errorf("failed to prepare talos registry manager: %w", err)
	}

	if registryMgr == nil {
		return nil
	}

	// TalosInDocker uses the cluster name as the network name
	networkName := clusterName
	if networkName == "" && talosConfig != nil {
		networkName = talosConfig.Name
	}

	if networkName == "" {
		networkName = talosconfigmanager.DefaultClusterName
	}

	errSetup := registry.SetupRegistries(
		ctx,
		registryMgr,
		registriesInfo,
		clusterName,
		networkName,
		writer,
	)
	if errSetup != nil {
		return fmt.Errorf("failed to setup talos registries: %w", errSetup)
	}

	return nil
}

// ConnectRegistriesToNetwork connects existing registries to the TalosInDocker network.
// This should be called after the TalosInDocker cluster is created and the network exists.
func ConnectRegistriesToNetwork(
	ctx context.Context,
	mirrorSpecs []registry.MirrorSpec,
	clusterName string,
	dockerClient client.APIClient,
	writer io.Writer,
) error {
	if len(mirrorSpecs) == 0 {
		return nil
	}

	registriesInfo := buildRegistryInfosFromSpecs(mirrorSpecs, nil, nil)
	if len(registriesInfo) == 0 {
		return nil
	}

	// TalosInDocker uses the cluster name as the network name
	networkName := clusterName
	if networkName == "" {
		networkName = talosconfigmanager.DefaultClusterName
	}

	errConnect := registry.ConnectRegistriesToNetwork(
		ctx,
		dockerClient,
		registriesInfo,
		networkName,
		writer,
	)
	if errConnect != nil {
		return fmt.Errorf("failed to connect talos registries to network: %w", errConnect)
	}

	return nil
}

// CleanupRegistries removes registries that are no longer in use.
func CleanupRegistries(
	ctx context.Context,
	mirrorSpecs []registry.MirrorSpec,
	clusterName string,
	dockerClient client.APIClient,
	deleteVolumes bool,
) error {
	registryMgr, registriesInfo, err := prepareTalosRegistryManager(ctx, mirrorSpecs, dockerClient)
	if err != nil {
		return fmt.Errorf("failed to prepare registry manager for cleanup: %w", err)
	}

	if registryMgr == nil {
		return nil
	}

	// TalosInDocker uses the cluster name as the network name
	networkName := clusterName
	if networkName == "" {
		networkName = talosconfigmanager.DefaultClusterName
	}

	errCleanup := registry.CleanupRegistries(
		ctx,
		registryMgr,
		registriesInfo,
		clusterName,
		deleteVolumes,
		networkName,
		nil,
	)
	if errCleanup != nil {
		return fmt.Errorf("failed to cleanup talos registries: %w", errCleanup)
	}

	return nil
}

// prepareTalosRegistryManager creates a registry manager and info slice from mirror specs.
func prepareTalosRegistryManager(
	_ context.Context,
	mirrorSpecs []registry.MirrorSpec,
	dockerClient client.APIClient,
) (*dockerclient.RegistryManager, []registry.Info, error) {
	if len(mirrorSpecs) == 0 {
		return nil, nil, nil
	}

	registryMgr, err := dockerclient.NewRegistryManager(dockerClient)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create registry manager: %w", err)
	}

	registriesInfo := buildRegistryInfosFromSpecs(mirrorSpecs, nil, nil)

	if len(registriesInfo) == 0 {
		return nil, nil, nil
	}

	return registryMgr, registriesInfo, nil
}

// buildRegistryInfosFromSpecs builds registry info from mirror specs directly.
func buildRegistryInfosFromSpecs(
	mirrorSpecs []registry.MirrorSpec,
	upstreams map[string]string,
	baseUsedPorts map[int]struct{},
) []registry.Info {
	registryInfos := make([]registry.Info, 0, len(mirrorSpecs))

	usedPorts, nextPort := registry.InitPortAllocation(baseUsedPorts)

	for _, spec := range mirrorSpecs {
		host := strings.TrimSpace(spec.Host)
		if host == "" {
			continue
		}

		// Build endpoint for this host
		port := registry.AllocatePort(&nextPort, usedPorts)
		endpoint := "http://" + net.JoinHostPort(host, strconv.Itoa(port))

		// Get upstream URL
		upstream := spec.Remote
		if upstream == "" {
			upstream = registry.GenerateUpstreamURL(host)
		}

		if upstreams != nil && upstreams[host] != "" {
			upstream = upstreams[host]
		}

		info := registry.BuildRegistryInfo(host, []string{endpoint}, port, "", upstream)
		registryInfos = append(registryInfos, info)
	}

	return registryInfos
}
