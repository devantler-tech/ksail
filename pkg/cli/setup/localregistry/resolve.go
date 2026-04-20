package localregistry

import (
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/client/oci"
	k3dconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/k3d"
	kindconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/kind"
	talosconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/registry"
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
	distribution := clusterCfg.Spec.Cluster.Distribution
	if name, handled := resolveCoreDistributionName(
		distribution,
		clusterCfg,
		kindConfig,
		k3dConfig,
		talosConfig,
	); handled {
		return name
	}

	return resolveAuxDistributionName(distribution, clusterCfg, vclusterConfig)
}

// resolveCoreDistributionName handles the distributions that delegate name
// resolution to their respective configmanager packages.
func resolveCoreDistributionName(
	distribution v1alpha1.Distribution,
	clusterCfg *v1alpha1.Cluster,
	kindConfig *kindv1alpha4.Cluster,
	k3dConfig *k3dv1alpha5.SimpleConfig,
	talosConfig *talosconfigmanager.Configs,
) (string, bool) {
	switch distribution {
	case v1alpha1.DistributionVanilla:
		return kindconfigmanager.ResolveClusterName(clusterCfg, kindConfig), true
	case v1alpha1.DistributionK3s:
		return k3dconfigmanager.ResolveClusterName(clusterCfg, k3dConfig), true
	case v1alpha1.DistributionTalos:
		return talosconfigmanager.ResolveClusterName(clusterCfg, talosConfig), true
	case v1alpha1.DistributionVCluster,
		v1alpha1.DistributionKWOK,
		v1alpha1.DistributionEKS:
		return "", false
	}

	return "", false
}

// resolveAuxDistributionName handles distributions without a dedicated
// configmanager name resolver.
func resolveAuxDistributionName(
	distribution v1alpha1.Distribution,
	clusterCfg *v1alpha1.Cluster,
	vclusterConfig *clusterprovisioner.VClusterConfig,
) string {
	switch distribution {
	case v1alpha1.DistributionVCluster:
		if vclusterConfig != nil {
			if name := strings.TrimSpace(vclusterConfig.GetClusterName()); name != "" {
				return name
			}
		}

		return "vcluster-default"
	case v1alpha1.DistributionKWOK:
		ctx := strings.TrimSpace(clusterCfg.Spec.Cluster.Connection.Context)
		if name, ok := strings.CutPrefix(ctx, "kwok-"); ok {
			if name = strings.TrimSpace(name); name != "" {
				return name
			}
		}

		return "kwok-default"
	case v1alpha1.DistributionEKS:
		if name := parseEKSContext(clusterCfg.Spec.Cluster.Connection.Context); name != "" {
			return name
		}

		return "eks-default"
	case v1alpha1.DistributionVanilla,
		v1alpha1.DistributionK3s,
		v1alpha1.DistributionTalos:
		return "ksail"
	}

	return "ksail"
}

func resolveNetworkName(
	clusterCfg *v1alpha1.Cluster,
	clusterName string,
) string {
	switch clusterCfg.Spec.Cluster.Distribution {
	case v1alpha1.DistributionVanilla:
		return "kind"
	case v1alpha1.DistributionK3s:
		return "k3d-" + trimOrDefault(clusterName, "k3d")
	case v1alpha1.DistributionTalos:
		return trimOrDefault(clusterName, "talos-default")
	case v1alpha1.DistributionVCluster:
		return "vcluster." + trimOrDefault(clusterName, "vcluster-default")
	case v1alpha1.DistributionKWOK:
		return "kwok-" + trimOrDefault(clusterName, "kwok-default")
	case v1alpha1.DistributionEKS:
		// EKS does not use a local Docker network.
		return ""
	default:
		return ""
	}
}

func trimOrDefault(name, fallback string) string {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return fallback
	}

	return trimmed
}

// parseEKSContext extracts the cluster name from an EKS kubeconfig context.
// eksctl-produced contexts look like "<iam-identity>@<name>.<region>.eksctl.io";
// bare "<name>.eksctl.io" contexts are also common. Returns an empty string
// when the context is not recognisably an EKS context so the caller can fall
// back to a default.
func parseEKSContext(ctx string) string {
	ctx = strings.TrimSpace(ctx)
	if ctx == "" {
		return ""
	}

	if idx := strings.LastIndex(ctx, "@"); idx >= 0 && idx+1 < len(ctx) {
		ctx = ctx[idx+1:]
	}

	trimmed, ok := strings.CutSuffix(ctx, ".eksctl.io")
	if !ok {
		return ""
	}

	if idx := strings.Index(trimmed, "."); idx >= 0 {
		trimmed = trimmed[:idx]
	}

	return strings.TrimSpace(trimmed)
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
