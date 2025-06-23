using DevantlerTech.KubernetesProvisioner.Cluster.Core;
using DevantlerTech.KubernetesProvisioner.Cluster.K3d;
using DevantlerTech.KubernetesProvisioner.Cluster.Kind;
using KSail.Models;
using KSail.Models.Project.Enums;

namespace KSail.Commands.Stop.Handlers;

class KSailStopCommandHandler : ICommandHandler
{
  readonly KSailCluster _config;
  readonly IKubernetesClusterProvisioner _clusterProvisioner;

  internal KSailStopCommandHandler(KSailCluster config)
  {
    _config = config;
    _clusterProvisioner = _config.Spec.Project.Distribution switch
    {
      KSailDistributionType.Kind => new KindProvisioner(),
      KSailDistributionType.K3d => new K3dProvisioner(),
      _ => throw new NotSupportedException($"The distribution '{_config.Spec.Project.Distribution}' is not supported.")
    };
  }

  public async Task<int> HandleAsync(CancellationToken cancellationToken = default)
  {
    Console.WriteLine($"⏹️ Stopping cluster...");
    Console.WriteLine($"► stopping cluster '{_config.Spec.Connection.Context}'");
    await _clusterProvisioner.StopAsync(_config.Metadata.Name, cancellationToken).ConfigureAwait(false);
    Console.WriteLine("✔ cluster stopped");
    return 0;
  }
}
