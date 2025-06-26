using DevantlerTech.KubernetesProvisioner.Cluster.K3d;
using DevantlerTech.KubernetesProvisioner.Cluster.Kind;
using KSail.Models;
using KSail.Models.Project.Enums;

namespace KSail.Commands.List.Handlers;

sealed class KSailListCommandHandler(KSailCluster config) : ICommandHandler
{
  readonly KSailCluster _config = config;
  readonly K3dProvisioner _k3dProvisioner = new();
  readonly KindProvisioner _kindProvisioner = new();

  public async Task HandleAsync(CancellationToken cancellationToken = default)
  {
    IEnumerable<string> clusters;
    if (_config.Spec.Distribution.ShowAllClustersInListings)
    {
      Console.WriteLine("---- K3d ----");
      clusters = [.. await _k3dProvisioner.ListAsync(cancellationToken).ConfigureAwait(false)];
      PrintClusters(clusters);
      Console.WriteLine();

      Console.WriteLine("---- Kind ----");
      clusters = [.. await _kindProvisioner.ListAsync(cancellationToken).ConfigureAwait(false)];
      PrintClusters(clusters);
    }
    else
    {
      clusters = _config.Spec.Project.Distribution switch
      {
        KSailDistributionType.K3d => await _k3dProvisioner.ListAsync(cancellationToken).ConfigureAwait(false),
        KSailDistributionType.Kind => await _kindProvisioner.ListAsync(cancellationToken).ConfigureAwait(false),
        _ => throw new NotSupportedException($"The container engine '{_config.Spec.Project.ContainerEngine}' and distribution '{_config.Spec.Project.Distribution}' combination is not supported.")
      };
      PrintClusters(clusters);
    }
  }

  static void PrintClusters(IEnumerable<string> clusters)
  {
    var clusterList = clusters.ToList();
    if (clusterList.Count != 0)
    {
      foreach (string? cluster in clusterList)
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
