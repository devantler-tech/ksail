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
  readonly DockerProvisioner _containerEngineProvisioner;
  readonly IKubernetesClusterProvisioner _kubernetesDistributionProvisioner;

  internal KSailDownCommandHandler(KSailCluster config)
  {
    _config = config;
    _containerEngineProvisioner = _config.Spec.Project.Provider switch
    {
      KSailProviderType.Docker or KSailProviderType.Podman => new DockerProvisioner(),
      _ => throw new KSailException($"Provider '{_config.Spec.Project.Provider}' is not supported.")
    };
    _kubernetesDistributionProvisioner = _config.Spec.Project.Distribution switch
    {
      KSailDistributionType.K3s => new K3dProvisioner(),
      KSailDistributionType.Native => new KindProvisioner(),
      _ => throw new KSailException($"Kubernetes distribution '{_config.Spec.Project.Provider}' is not supported.")
    };
  }

  internal async Task<bool> HandleAsync(CancellationToken cancellationToken = default)
  {
    Console.WriteLine($"🔥 Destroying cluster...");
    await _kubernetesDistributionProvisioner.DeleteAsync(_config.Metadata.Name, cancellationToken).ConfigureAwait(false);
    await DeleteRegistriesAsync(cancellationToken).ConfigureAwait(false);
    return true;
  }

  async Task DeleteRegistriesAsync(CancellationToken cancellationToken = default)
  {
    string containerName = _config.Spec.LocalRegistry.Name;
    bool ksailRegistryExists = await _containerEngineProvisioner.CheckContainerExistsAsync(containerName, cancellationToken).ConfigureAwait(false);
    if (ksailRegistryExists)
    {
      Console.WriteLine("► Deleting local registry");
      await _containerEngineProvisioner.DeleteRegistryAsync(containerName, cancellationToken).ConfigureAwait(false);
      Console.WriteLine($"✓ '{containerName}' deleted.");
    }

    Console.WriteLine("► Deleting mirror registries");
    if (_config.Spec.Project.MirrorRegistries)
    {
      var deleteTasks = _config.Spec.MirrorRegistries.Select(async mirrorRegistry =>
      {
        bool mirrorRegistryExists = await _containerEngineProvisioner.CheckContainerExistsAsync(mirrorRegistry.Name, cancellationToken).ConfigureAwait(false);
        if (mirrorRegistryExists)
        {
          await _containerEngineProvisioner.DeleteRegistryAsync(mirrorRegistry.Name, cancellationToken).ConfigureAwait(false);
          Console.WriteLine($"✓ '{mirrorRegistry.Name}' deleted.");
        }
      });
      await Task.WhenAll(deleteTasks).ConfigureAwait(false);
    }
  }
}
