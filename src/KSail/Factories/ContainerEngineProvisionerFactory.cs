using Devantler.ContainerEngineProvisioner.Docker;
using Devantler.KubernetesProvisioner.Cluster.Core;
using Devantler.KubernetesProvisioner.Cluster.K3d;
using Devantler.KubernetesProvisioner.Cluster.Kind;
using KSail.Models;
using KSail.Models.Project.Enums;

namespace KSail.Factories;

class ContainerEngineProvisionerFactory
{
  internal static DockerProvisioner Create(KSailCluster config)
  {
    switch (config.Spec.Project.Provider)
    {
      case KSailProviderType.Docker:
        return new DockerProvisioner();
      case KSailProviderType.Podman:
        string socketPath = PodmanHelper.GetPodmanSocket();
        return new DockerProvisioner(socketPath);
      default:
        throw new NotSupportedException($"The provider '{config.Spec.Project.Provider}' is not supported.");
    }
  }
}
