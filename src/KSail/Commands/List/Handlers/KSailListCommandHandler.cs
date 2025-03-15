using Devantler.KubernetesProvisioner.Cluster.K3d;
using Devantler.KubernetesProvisioner.Cluster.Kind;
using KSail.Models;
using KSail.Models.Project.Enums;

namespace KSail.Commands.List.Handlers;

sealed class KSailListCommandHandler(KSailCluster config)
{
  readonly KSailCluster _config = config;
  readonly K3dProvisioner _k3dProvisioner = new();
  readonly KindProvisioner _kindProvisioner = new();

  internal async Task<bool> HandleAsync(CancellationToken cancellationToken = default)
  {
    IEnumerable<string> clusters = [];
    if (_config.Spec.Distribution.ShowAllClustersInListings)
    {
      Console.WriteLine("---- K3d ----");
      clusters = [.. clusters, .. await _k3dProvisioner.ListAsync(cancellationToken).ConfigureAwait(false)];
      PrintClusters(clusters);
      Console.WriteLine();

      Console.WriteLine("---- Kind ----");
      clusters = [.. clusters, .. await _kindProvisioner.ListAsync(cancellationToken).ConfigureAwait(false)];
      clusters = clusters.Where(cluster => !cluster.Contains("No kind clusters found.", StringComparison.Ordinal));
      PrintClusters(clusters);
      return true;
    }
    else
    {
      clusters = (_config.Spec.Project.Engine, _config.Spec.Project.Distribution) switch
      {
        (KSailEngineType.Docker, KSailKubernetesDistributionType.K3s) => await _k3dProvisioner.ListAsync(cancellationToken).ConfigureAwait(false),
        (KSailEngineType.Docker, KSailKubernetesDistributionType.Native) => await _kindProvisioner.ListAsync(cancellationToken).ConfigureAwait(false),
        _ => throw new NotSupportedException($"The container engine '{_config.Spec.Project.Engine}' and distribution '{_config.Spec.Project.Distribution}' combination is not supported.")
      };
      clusters = clusters.Where(cluster => !cluster.Contains("No kind clusters found.", StringComparison.Ordinal));
      PrintClusters(clusters);
      return true;
    }
  }

  private static void PrintClusters(IEnumerable<string> clusters)
  {
    var clusterList = clusters.ToList();
    if (clusterList.Count != 0)
    {
      foreach (var cluster in clusterList)
      {
        Console.WriteLine(cluster);
      }
    }
    else
    {
      Console.WriteLine("No clusters found.");
    }
  }
}
