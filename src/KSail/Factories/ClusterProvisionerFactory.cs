using System.Runtime.InteropServices;
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
    switch (config.Spec.Project.ContainerEngine)
    {
      case KSailContainerEngineType.Docker:
        return GetKubernetesClusterProvisioner(config);
      case KSailContainerEngineType.Podman:
        string socketPath = PodmanHelper.GetPodmanSocket();
        Environment.SetEnvironmentVariable("DOCKER_HOST", socketPath);
        return GetKubernetesClusterProvisioner(config);
      default:
        throw new NotSupportedException($"The provider '{config.Spec.Project.ContainerEngine}' is not supported.");
    }


  }

  static IKubernetesClusterProvisioner GetKubernetesClusterProvisioner(KSailCluster config)
  {
    return config.Spec.Project.Distribution switch
    {
      KSailDistributionType.Kind => new KindProvisioner(),
      KSailDistributionType.K3d => new K3dProvisioner(),
      _ => throw new NotSupportedException($"The distribution '{config.Spec.Project.Distribution}' is not supported.")
    };
  }
}
