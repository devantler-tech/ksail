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
    return config.Spec.Project.Provider switch
    {
      KSailProviderType.Docker or KSailProviderType.Podman => new DockerProvisioner(),
      _ => throw new NotSupportedException($"The container engine '{config.Spec.Project.Provider}' is not supported.")
    };
  }
}
