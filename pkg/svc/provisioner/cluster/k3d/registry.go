package k3dprovisioner

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	dockerclient "github.com/devantler-tech/ksail/v5/pkg/client/docker"
	k3dconfigmanager "github.com/devantler-tech/ksail/v5/pkg/io/config-manager/k3d"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/registry"
	"github.com/docker/docker/client"
	k3dv1alpha5 "github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	"sigs.k8s.io/yaml"
)

// SetupRegistries creates mirror registries based on the K3d simple configuration.
func SetupRegistries(
	ctx context.Context,
	simpleCfg *k3dv1alpha5.SimpleConfig,
	clusterName string,
	dockerClient client.APIClient,
	writer io.Writer,
) error {
	registryMgr, registryInfos, networkName, err := prepareRegistryContext(
		ctx, simpleCfg, clusterName, dockerClient,
	)
	if err != nil || registryMgr == nil {
		return err
	}

	errRegistry := registry.SetupRegistries(
		ctx,
		registryMgr,
		registryInfos,
		clusterName,
		networkName,
		writer,
	)
	if errRegistry != nil {
		return fmt.Errorf("failed to setup k3d registries: %w", errRegistry)
	}

	return nil
}

// ConnectRegistriesToNetwork attaches registry containers to the K3d cluster network.
func ConnectRegistriesToNetwork(
	ctx context.Context,
	simpleCfg *k3dv1alpha5.SimpleConfig,
	clusterName string,
	dockerClient client.APIClient,
	writer io.Writer,
) error {
	if simpleCfg == nil {
		return nil
	}

	registryInfos := extractRegistriesFromConfig(simpleCfg, nil)
	if len(registryInfos) == 0 {
		return nil
	}

	networkName := k3dconfigmanager.ResolveNetworkName(clusterName)

	errConnect := registry.ConnectRegistriesToNetwork(
		ctx,
		dockerClient,
		registryInfos,
		networkName,
		writer,
	)
	if errConnect != nil {
		return fmt.Errorf("failed to connect k3d registries to network: %w", errConnect)
	}

	return nil
}

// CleanupRegistries removes registry containers associated with the cluster.
func CleanupRegistries(
	ctx context.Context,
	simpleCfg *k3dv1alpha5.SimpleConfig,
	clusterName string,
	dockerClient client.APIClient,
	deleteVolumes bool,
	writer io.Writer,
) error {
	registryMgr, registryInfos, networkName, err := prepareRegistryContext(
		ctx, simpleCfg, clusterName, dockerClient,
	)
	if err != nil || registryMgr == nil {
		return err
	}

	errCleanup := registry.CleanupRegistries(
		ctx,
		registryMgr,
		registryInfos,
		clusterName,
		deleteVolumes,
		networkName,
		writer,
	)
	if errCleanup != nil {
		return fmt.Errorf("failed to cleanup k3d registries: %w", errCleanup)
	}

	return nil
}

// prepareRegistryContext sets up the registry manager and resolves the network name.
// Returns nil manager if no registries are configured.
func prepareRegistryContext(
	ctx context.Context,
	simpleCfg *k3dv1alpha5.SimpleConfig,
	clusterName string,
	dockerClient client.APIClient,
) (*dockerclient.RegistryManager, []registry.Info, string, error) {
	registryMgr, registryInfos, err := setupRegistryManager(ctx, simpleCfg, dockerClient)
	if err != nil {
		return nil, nil, "", err
	}

	if registryMgr == nil {
		return nil, nil, "", nil
	}

	networkName := k3dconfigmanager.ResolveNetworkName(clusterName)

	return registryMgr, registryInfos, networkName, nil
}

func setupRegistryManager(
	ctx context.Context,
	simpleCfg *k3dv1alpha5.SimpleConfig,
	dockerClient client.APIClient,
) (*dockerclient.RegistryManager, []registry.Info, error) {
	if simpleCfg == nil {
		return nil, nil, nil
	}

	registryMgr, infos, err := registry.PrepareRegistryManager(
		ctx,
		dockerClient,
		func(usedPorts map[int]struct{}) []registry.Info {
			return extractRegistriesFromConfig(simpleCfg, usedPorts)
		},
	)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to prepare k3d registry manager: %w", err)
	}

	return registryMgr, infos, nil
}

type mirrorConfig struct {
	Endpoint []string `yaml:"endpoint"`
}

type k3dMirrorConfig struct {
	Mirrors map[string]mirrorConfig `yaml:"mirrors"`
}

func extractRegistriesFromConfig(
	simpleCfg *k3dv1alpha5.SimpleConfig,
	baseUsedPorts map[int]struct{},
) []registry.Info {
	if simpleCfg == nil {
		return nil
	}

	mirrorCfg := parseMirrorConfig(simpleCfg.Registries.Config)
	if mirrorCfg == nil || len(mirrorCfg.Mirrors) == 0 {
		return nil
	}

	nativeRegistryHost := resolveNativeRegistryHost(simpleCfg)
	hosts := filterMirrorHosts(mirrorCfg.Mirrors, nativeRegistryHost)

	if len(hosts) == 0 {
		return nil
	}

	return buildRegistryInfos(hosts, mirrorCfg.Mirrors, baseUsedPorts)
}

// parseMirrorConfig parses the K3d registries.config YAML string.
func parseMirrorConfig(configStr string) *k3dMirrorConfig {
	trimmed := strings.TrimSpace(configStr)
	if trimmed == "" {
		return nil
	}

	var mirrorCfg k3dMirrorConfig

	err := yaml.Unmarshal([]byte(trimmed), &mirrorCfg)
	if err != nil {
		return nil
	}

	return &mirrorCfg
}

// resolveNativeRegistryHost returns the host:port for K3d-native registry, or empty string.
func resolveNativeRegistryHost(simpleCfg *k3dv1alpha5.SimpleConfig) string {
	if simpleCfg.Registries.Create == nil || simpleCfg.Registries.Create.Name == "" {
		return ""
	}

	return simpleCfg.Registries.Create.Name + ":" + strconv.Itoa(dockerclient.DefaultRegistryPort)
}

// filterMirrorHosts returns sorted hosts excluding the native registry.
func filterMirrorHosts(mirrors map[string]mirrorConfig, nativeHost string) []string {
	hosts := make([]string, 0, len(mirrors))

	for host := range mirrors {
		if nativeHost != "" && host == nativeHost {
			continue
		}

		hosts = append(hosts, host)
	}

	registry.SortHosts(hosts)

	return hosts
}

// buildRegistryInfos creates registry.Info slices from mirror config.
func buildRegistryInfos(
	hosts []string,
	mirrors map[string]mirrorConfig,
	baseUsedPorts map[int]struct{},
) []registry.Info {
	usedPorts, nextPort := registry.InitPortAllocation(baseUsedPorts)
	registryInfos := make([]registry.Info, 0, len(hosts))

	for _, host := range hosts {
		endpoints := mirrors[host].Endpoint
		port := registry.ExtractRegistryPort(endpoints, usedPorts, &nextPort)
		upstream := upstreamFromEndpoints(host, endpoints)
		info := registry.BuildRegistryInfo(host, endpoints, port, "", upstream)
		registryInfos = append(registryInfos, info)
	}

	return registryInfos
}

// ExtractRegistriesFromConfigForTesting exposes registry extraction for testing and callers that need inspection.
func ExtractRegistriesFromConfigForTesting(simpleCfg *k3dv1alpha5.SimpleConfig) []registry.Info {
	return extractRegistriesFromConfig(simpleCfg, nil)
}

func upstreamFromEndpoints(host string, endpoints []string) string {
	if len(endpoints) == 0 {
		return ""
	}

	expectedLocal := registry.BuildRegistryName("", host)

	for idx := len(endpoints) - 1; idx >= 0; idx-- {
		candidate := strings.TrimSpace(endpoints[idx])
		if candidate == "" {
			continue
		}

		switch extracted := registry.ExtractNameFromEndpoint(candidate); {
		case extracted == "":
			return candidate
		case extracted != expectedLocal:
			return candidate
		}
	}

	return ""
}
