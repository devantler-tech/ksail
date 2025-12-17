package k3dprovisioner

import (
	"context"
	"fmt"
	"io"
	"strings"

	dockerclient "github.com/devantler-tech/ksail-go/pkg/client/docker"
	"github.com/devantler-tech/ksail-go/pkg/svc/provisioner/registry"
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
	registryMgr, registryInfos, err := setupRegistryManager(ctx, simpleCfg, dockerClient)
	if err != nil {
		return err
	}

	if registryMgr == nil {
		return nil
	}

	networkName := resolveK3dNetworkName(clusterName)

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

	networkName := resolveK3dNetworkName(clusterName)

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
	registryMgr, registryInfos, err := setupRegistryManager(ctx, simpleCfg, dockerClient)
	if err != nil {
		return err
	}

	if registryMgr == nil {
		return nil
	}

	networkName := resolveK3dNetworkName(clusterName)

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

func resolveK3dNetworkName(clusterName string) string {
	trimmed := strings.TrimSpace(clusterName)
	if trimmed == "" {
		return "k3d"
	}

	return "k3d-" + trimmed
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

	configStr := strings.TrimSpace(simpleCfg.Registries.Config)
	if configStr == "" {
		return nil
	}

	var mirrorCfg k3dMirrorConfig

	err := yaml.Unmarshal([]byte(configStr), &mirrorCfg)
	if err != nil {
		return nil
	}

	if len(mirrorCfg.Mirrors) == 0 {
		return nil
	}

	hosts := make([]string, 0, len(mirrorCfg.Mirrors))
	for host := range mirrorCfg.Mirrors {
		hosts = append(hosts, host)
	}

	registry.SortHosts(hosts)

	usedPorts, nextPort := registry.InitPortAllocation(baseUsedPorts)

	registryInfos := make([]registry.Info, 0, len(hosts))

	for _, host := range hosts {
		endpoints := mirrorCfg.Mirrors[host].Endpoint
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
