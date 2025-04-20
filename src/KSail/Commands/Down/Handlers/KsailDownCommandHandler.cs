using System.ComponentModel;
using Devantler.ContainerEngineProvisioner.Core;
using Devantler.ContainerEngineProvisioner.Docker;
using Devantler.KubernetesProvisioner.Cluster.Core;
using Devantler.KubernetesProvisioner.Cluster.K3d;
using Devantler.KubernetesProvisioner.Cluster.Kind;
using KSail.Factories;
using KSail.Models;
using KSail.Models.Project.Enums;

namespace KSail.Commands.Down.Handlers;

class KSailDownCommandHandler(KSailCluster config)
{
  readonly KSailCluster _config = config;
  readonly IContainerEngineProvisioner _containerEngineProvisioner = ContainerEngineProvisionerFactory.Create(config);
  readonly IKubernetesClusterProvisioner _kubernetesDistributionProvisioner = ClusterProvisionerFactory.Create(config);

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
