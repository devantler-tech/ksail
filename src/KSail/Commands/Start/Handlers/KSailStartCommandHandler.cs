using DevantlerTech.KubernetesProvisioner.Cluster.Core;
using DevantlerTech.KubernetesProvisioner.Cluster.K3d;
using DevantlerTech.KubernetesProvisioner.Cluster.Kind;
using KSail.Models;
using KSail.Models.Project.Enums;

namespace KSail.Commands.Start.Handlers;

class KSailStartCommandHandler : ICommandHandler
{
  readonly KSailCluster _config;
  readonly IKubernetesClusterProvisioner _clusterProvisioner;

  internal KSailStartCommandHandler(KSailCluster config)
  {
    _config = config;
    _clusterProvisioner = _config.Spec.Project.Distribution switch
    {
      KSailDistributionType.Kind => new KindProvisioner(),
      KSailDistributionType.K3d => new K3dProvisioner(),
      _ => throw new NotSupportedException($"The distribution '{_config.Spec.Project.Distribution}' combination is not supported")
    };
  }

  public async Task HandleAsync(CancellationToken cancellationToken = default)
  {
    Console.WriteLine($"▶️ Starting cluster...");
    Console.WriteLine($"► starting cluster '{_config.Spec.Connection.Context}'");
    await _clusterProvisioner.StartAsync(_config.Metadata.Name, cancellationToken).ConfigureAwait(false);
    Console.WriteLine("✔ cluster started");
    return 0;
  }
}
