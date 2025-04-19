using Devantler.KubernetesProvisioner.Cluster.Core;
using Devantler.KubernetesProvisioner.Cluster.K3d;
using Devantler.KubernetesProvisioner.Cluster.Kind;
using KSail.Models;
using KSail.Models.Project.Enums;

namespace KSail.Factories;

class ClusterProvisionerFactory
{
  internal static IKubernetesClusterProvisioner Create(KSailCluster config)
  {
    return (config.Spec.Project.Provider, config.Spec.Project.Distribution) switch
    {
      (KSailProviderType.Docker or KSailProviderType.Podman, KSailDistributionType.Native) => new KindProvisioner(),
      (KSailProviderType.Docker or KSailProviderType.Podman, KSailDistributionType.K3s) => new K3dProvisioner(),
      _ => throw new NotSupportedException($"The distribution '{config.Spec.Project.Distribution}' is not supported.")
    };
  }
}
