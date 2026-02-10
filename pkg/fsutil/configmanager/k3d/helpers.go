package k3d

import (
	"strings"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	v1alpha5 "github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	"sigs.k8s.io/yaml"
)

// mirrorConfigEntry represents a single mirror registry configuration entry in K3d.
type mirrorConfigEntry struct {
	Endpoint []string `yaml:"endpoint"`
}

// mirrorConfig represents the mirrors section of K3d's registry configuration.
type mirrorConfig struct {
	Mirrors map[string]mirrorConfigEntry `yaml:"mirrors"`
}

// ParseRegistryConfig parses K3d registry mirror configuration from raw YAML string.
// Returns a map of host to endpoints, filtering out empty entries.
// Intentionally returns an empty map (instead of an error) for invalid YAML to support
// graceful degradation when registry configuration is malformed or missing.
func ParseRegistryConfig(raw string) map[string][]string {
	result := make(map[string][]string)

	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return result
	}

	var cfg mirrorConfig

	err := yaml.Unmarshal([]byte(trimmed), &cfg)
	if err != nil {
		return result
	}

	for host, entry := range cfg.Mirrors {
		if len(entry.Endpoint) == 0 {
			continue
		}

		filtered := make([]string, 0, len(entry.Endpoint))
		for _, endpoint := range entry.Endpoint {
			endpoint = strings.TrimSpace(endpoint)
			if endpoint == "" {
				continue
			}

			filtered = append(filtered, endpoint)
		}

		if len(filtered) == 0 {
			continue
		}

		result[host] = filtered
	}

	return result
}

// ResolveClusterName returns the effective cluster name from K3d config or cluster config.
// Priority: k3dConfig.Name > clusterCfg.Spec.Cluster.Connection.Context > DefaultClusterName.
// Returns DefaultClusterName if both configs are nil or have empty names.
func ResolveClusterName(
	clusterCfg *v1alpha1.Cluster,
	k3dConfig *v1alpha5.SimpleConfig,
) string {
	if k3dConfig != nil {
		if name := strings.TrimSpace(k3dConfig.Name); name != "" {
			return name
		}
	}

	if clusterCfg != nil {
		if name := strings.TrimSpace(clusterCfg.Spec.Cluster.Connection.Context); name != "" {
			return name
		}
	}

	return DefaultClusterName
}

// ResolveNetworkName returns the Docker network name for a K3d cluster.
// K3d uses "k3d-<clustername>" as the network naming convention.
func ResolveNetworkName(clusterName string) string {
	trimmed := strings.TrimSpace(clusterName)
	if trimmed == "" {
		return "k3d"
	}

	return "k3d-" + trimmed
}
