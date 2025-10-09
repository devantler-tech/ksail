using System.CommandLine;
using System.Threading.Tasks;
using DevantlerTech.KubernetesProvisioner.Cluster.K3d;
using DevantlerTech.KubernetesProvisioner.Cluster.Kind;
using KSail.Models;
using KSail.Models.Project.Enums;

namespace KSail.Commands.List.Handlers;

sealed class KSailListCommandHandler(KSailCluster config, ParseResult parseResult) : ICommandHandler
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
      await PrintClustersAsync(clusters).ConfigureAwait(false);
      Console.WriteLine();

      Console.WriteLine("---- Kind ----");
      clusters = [.. await _kindProvisioner.ListAsync(cancellationToken).ConfigureAwait(false)];
      await PrintClustersAsync(clusters).ConfigureAwait(false);
    }
    else
    {
      clusters = _config.Spec.Project.Distribution switch
      {
        KSailDistributionType.K3d => await _k3dProvisioner.ListAsync(cancellationToken).ConfigureAwait(false),
        KSailDistributionType.Kind => await _kindProvisioner.ListAsync(cancellationToken).ConfigureAwait(false),
        _ => throw new NotSupportedException($"The container engine '{_config.Spec.Project.ContainerEngine}' and distribution '{_config.Spec.Project.Distribution}' combination is not supported.")
      };
      await PrintClustersAsync(clusters).ConfigureAwait(false);
    }
  }

  async Task PrintClustersAsync(IEnumerable<string> clusters)
  {
    var clusterList = clusters.ToList();
    if (clusterList.Count != 0)
    {
      foreach (string? cluster in clusterList)
      {
        await parseResult.InvocationConfiguration.Output.WriteLineAsync(cluster).ConfigureAwait(false);
      }
    }
    else
    {
      Console.WriteLine("No clusters found.");
    }
  }
}
