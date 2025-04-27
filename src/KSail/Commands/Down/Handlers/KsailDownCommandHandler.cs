using System.ComponentModel;
using Devantler.ContainerEngineProvisioner.Core;
using Devantler.ContainerEngineProvisioner.Docker;
using Devantler.KubernetesProvisioner.Cluster.Core;
using Devantler.KubernetesProvisioner.Cluster.K3d;
using Devantler.KubernetesProvisioner.Cluster.Kind;
using k8s.KubeConfigModels;
using KSail.Factories;
using KSail.Managers;
using KSail.Models;
using KSail.Models.Project.Enums;

namespace KSail.Commands.Down.Handlers;

class KSailDownCommandHandler(KSailCluster config)
{
  readonly ClusterManager _clusterManager = new(config);
  readonly MirrorRegistryManager _mirrorRegistryManager = new(config);

  internal async Task<bool> HandleAsync(CancellationToken cancellationToken = default)
  {
    Console.WriteLine($"🔥 Destroying cluster...");
    await _clusterManager.CleanupAsync(cancellationToken).ConfigureAwait(false);
    await _mirrorRegistryManager.CleanupAsync(cancellationToken).ConfigureAwait(false);
    return true;
  }
}
