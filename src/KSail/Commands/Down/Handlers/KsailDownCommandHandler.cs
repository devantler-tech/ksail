using Devantler.ContainerEngineProvisioner.Docker;
using Devantler.KubernetesProvisioner.Cluster.Core;
using Devantler.KubernetesProvisioner.Cluster.K3d;
using Devantler.KubernetesProvisioner.Cluster.Kind;
using KSail.Models;
using KSail.Models.Project.Enums;

namespace KSail.Commands.Down.Handlers;

class KSailDownCommandHandler
{
  readonly KSailCluster _config;
  readonly DockerProvisioner _engineProvisioner;
  readonly IKubernetesClusterProvisioner _kubernetesDistributionProvisioner;

  internal KSailDownCommandHandler(KSailCluster config)
  {
    _config = config;
    _engineProvisioner = _config.Spec.Project.Engine switch
    {
      KSailEngineType.Docker => new DockerProvisioner(),
      _ => throw new KSailException($"Engine '{_config.Spec.Project.Engine}' is not supported.")
    };
    _kubernetesDistributionProvisioner = _config.Spec.Project.Distribution switch
    {
      KSailKubernetesDistributionType.K3s => new K3dProvisioner(),
      KSailKubernetesDistributionType.Native => new KindProvisioner(),
      _ => throw new KSailException($"Kubernetes distribution '{_config.Spec.Project.Engine}' is not supported.")
    };
  }

  internal async Task<bool> HandleAsync(CancellationToken cancellationToken = default)
  {
    await _kubernetesDistributionProvisioner.DeleteAsync(_config.Metadata.Name, cancellationToken).ConfigureAwait(false);
    await DeleteRegistriesAsync(cancellationToken).ConfigureAwait(false);
    return true;
  }

  async Task DeleteRegistriesAsync(CancellationToken cancellationToken = default)
  {
    switch (_config.Spec.Project.DeploymentTool)
    {
      case KSailDeploymentToolType.Flux:
        Console.WriteLine("► Deleting OCI source registry");
        string containerName = _config.Spec.LocalRegistry.Name;
        bool ksailRegistryExists = await _engineProvisioner.CheckContainerExistsAsync(containerName, cancellationToken).ConfigureAwait(false);
        if (ksailRegistryExists)
        {
          await _engineProvisioner.DeleteRegistryAsync(containerName, cancellationToken).ConfigureAwait(false);
          Console.WriteLine($"✓ '{containerName}' deleted.");
        }
        break;
      default:
        throw new KSailException($"deployment tool '{_config.Spec.Project.DeploymentTool}' is not supported.");
    }

    Console.WriteLine("► Deleting mirror registries");
    if (_config.Spec.Project.MirrorRegistries)
    {
      var deleteTasks = _config.Spec.MirrorRegistries.Select(async mirrorRegistry =>
      {
        bool mirrorRegistryExists = await _engineProvisioner.CheckContainerExistsAsync(mirrorRegistry.Name, cancellationToken).ConfigureAwait(false);
        if (mirrorRegistryExists)
        {
          await _engineProvisioner.DeleteRegistryAsync(mirrorRegistry.Name, cancellationToken).ConfigureAwait(false);
          Console.WriteLine($"✓ '{mirrorRegistry.Name}' deleted.");
        }
      });
      await Task.WhenAll(deleteTasks).ConfigureAwait(false);
    }
  }
}
