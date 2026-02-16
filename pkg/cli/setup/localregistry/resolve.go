package localregistry

import (
	"strings"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/client/oci"
	k3dconfigmanager "github.com/devantler-tech/ksail/v5/pkg/fsutil/configmanager/k3d"
	kindconfigmanager "github.com/devantler-tech/ksail/v5/pkg/fsutil/configmanager/kind"
	talosconfigmanager "github.com/devantler-tech/ksail/v5/pkg/fsutil/configmanager/talos"
	clusterprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/registry"
	k3dv1alpha5 "github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	kindv1alpha4 "sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

// buildVerifyOptions creates the OCI verify options from the cluster config.
func buildVerifyOptions(clusterCfg *v1alpha1.Cluster) oci.VerifyOptions {
	localRegistry := clusterCfg.Spec.Cluster.LocalRegistry
	parsed := localRegistry.Parse()
	username, password := localRegistry.ResolveCredentials()

	// Use the path as the repository for external registries
	repository := parsed.Path
	if repository == "" {
		repository = registry.SanitizeRepoName(clusterCfg.Spec.Workload.SourceDirectory)
		if repository == "" {
			repository = v1alpha1.DefaultSourceDirectory
		}
	}

	return oci.VerifyOptions{
		RegistryEndpoint: parsed.Host,
		Repository:       repository,
		Username:         username,
		Password:         password,
		Insecure:         false, // External registries use HTTPS
	}
}

func newRegistryContext(
	clusterCfg *v1alpha1.Cluster,
	kindConfig *kindv1alpha4.Cluster,
	k3dConfig *k3dv1alpha5.SimpleConfig,
	talosConfig *talosconfigmanager.Configs,
	vclusterConfig *clusterprovisioner.VClusterConfig,
) registryContext {
	clusterName := resolveClusterName(
		clusterCfg, kindConfig, k3dConfig, talosConfig, vclusterConfig,
	)
	networkName := resolveNetworkName(clusterCfg, clusterName)

	return registryContext{clusterName: clusterName, networkName: networkName}
}

func resolveClusterName(
	clusterCfg *v1alpha1.Cluster,
	kindConfig *kindv1alpha4.Cluster,
	k3dConfig *k3dv1alpha5.SimpleConfig,
	talosConfig *talosconfigmanager.Configs,
	vclusterConfig *clusterprovisioner.VClusterConfig,
) string {
	switch clusterCfg.Spec.Cluster.Distribution {
	case v1alpha1.DistributionVanilla:
		return kindconfigmanager.ResolveClusterName(clusterCfg, kindConfig)
	case v1alpha1.DistributionK3s:
		return k3dconfigmanager.ResolveClusterName(clusterCfg, k3dConfig)
	case v1alpha1.DistributionTalos:
		return talosconfigmanager.ResolveClusterName(clusterCfg, talosConfig)
	case v1alpha1.DistributionVCluster:
		if vclusterConfig != nil {
			if name := strings.TrimSpace(vclusterConfig.GetClusterName()); name != "" {
				return name
			}
		}

		return "vcluster-default"
	default:
		if name := strings.TrimSpace(clusterCfg.Spec.Cluster.Connection.Context); name != "" {
			return name
		}

		return "ksail"
	}
}

func resolveNetworkName(
	clusterCfg *v1alpha1.Cluster,
	clusterName string,
) string {
	switch clusterCfg.Spec.Cluster.Distribution {
	case v1alpha1.DistributionVanilla:
		return "kind"
	case v1alpha1.DistributionK3s:
		trimmed := strings.TrimSpace(clusterName)
		if trimmed == "" {
			trimmed = "k3d"
		}

		return "k3d-" + trimmed
	case v1alpha1.DistributionTalos:
		trimmed := strings.TrimSpace(clusterName)
		if trimmed == "" {
			trimmed = "talos-default"
		}

		return trimmed
	case v1alpha1.DistributionVCluster:
		trimmed := strings.TrimSpace(clusterName)
		if trimmed == "" {
			trimmed = "vcluster-default"
		}

		return "vcluster." + trimmed
	default:
		return ""
	}
}

func newCreateOptions(
	clusterCfg *v1alpha1.Cluster,
	ctx registryContext,
) registry.CreateOptions {
	return registry.CreateOptions{
		Name:        registry.BuildLocalRegistryName(ctx.clusterName),
		Host:        registry.DefaultEndpointHost,
		Port:        resolvePort(clusterCfg),
		ClusterName: ctx.clusterName,
		// Use base name for volume to share across clusters
		VolumeName: registry.LocalRegistryBaseName,
	}
}

func resolvePort(clusterCfg *v1alpha1.Cluster) int {
	return int(clusterCfg.Spec.Cluster.LocalRegistry.ResolvedPort())
}
