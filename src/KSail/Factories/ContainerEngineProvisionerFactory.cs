using Devantler.ContainerEngineProvisioner.Core;
using Devantler.ContainerEngineProvisioner.Docker;
using Devantler.ContainerEngineProvisioner.Podman;
using Devantler.KubernetesProvisioner.Cluster.Core;
using Devantler.KubernetesProvisioner.Cluster.K3d;
using Devantler.KubernetesProvisioner.Cluster.Kind;
using KSail.Models;
using KSail.Models.Project.Enums;

namespace KSail.Factories;

class ContainerEngineProvisionerFactory
{
  internal static IContainerEngineProvisioner Create(KSailCluster config)
  {
    return config.Spec.Project.ContainerEngine switch
    {
      KSailContainerEngineType.Docker => new DockerProvisioner(),
      KSailContainerEngineType.Podman => new PodmanProvisioner(),
      _ => throw new NotSupportedException($"The provider '{config.Spec.Project.ContainerEngine}' is not supported."),
    };
  }
}
