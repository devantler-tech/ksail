using DevantlerTech.ContainerEngineProvisioner.Core;
using DevantlerTech.ContainerEngineProvisioner.Docker;
using DevantlerTech.ContainerEngineProvisioner.Podman;
using DevantlerTech.KubernetesProvisioner.Cluster.Core;
using DevantlerTech.KubernetesProvisioner.Cluster.K3d;
using DevantlerTech.KubernetesProvisioner.Cluster.Kind;
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
