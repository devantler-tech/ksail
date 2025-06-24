using System.ComponentModel;
using DevantlerTech.ContainerEngineProvisioner.Core;
using DevantlerTech.ContainerEngineProvisioner.Docker;
using DevantlerTech.KubernetesProvisioner.Cluster.Core;
using DevantlerTech.KubernetesProvisioner.Cluster.K3d;
using DevantlerTech.KubernetesProvisioner.Cluster.Kind;
using k8s.KubeConfigModels;
using KSail.Factories;
using KSail.Managers;
using KSail.Models;
using KSail.Models.Project.Enums;

namespace KSail.Commands.Down.Handlers;

class KSailDownCommandHandler(KSailCluster config) : ICommandHandler
{
  readonly ClusterManager _clusterManager = new(config);
  readonly MirrorRegistryManager _mirrorRegistryManager = new(config);

  public async Task HandleAsync(CancellationToken cancellationToken = default)
  {
    Console.WriteLine($"🔥 Destroying cluster...");
    await _clusterManager.CleanupAsync(cancellationToken).ConfigureAwait(false);
    await _mirrorRegistryManager.CleanupAsync(cancellationToken).ConfigureAwait(false);
    return 0;
  }
}
