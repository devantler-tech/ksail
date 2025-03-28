using Devantler.KubernetesProvisioner.Cluster.Core;
using Devantler.KubernetesProvisioner.Cluster.K3d;
using Devantler.KubernetesProvisioner.Cluster.Kind;
using KSail.Models;
using KSail.Models.Project.Enums;

namespace KSail.Commands.Start.Handlers;

class KSailStartCommandHandler
{
  readonly KSailCluster _config;
  readonly IKubernetesClusterProvisioner _clusterProvisioner;

  internal KSailStartCommandHandler(KSailCluster config)
  {
    _config = config;
    _clusterProvisioner = (_config.Spec.Project.Provider, _config.Spec.Project.Distribution) switch
    {
      (KSailProviderType.Docker, KSailDistributionType.Native) => new KindProvisioner(),
      (KSailProviderType.Docker, KSailDistributionType.K3s) => new K3dProvisioner(),
      _ => throw new NotSupportedException($"The engine '{_config.Spec.Project.Provider}' and distribution '{_config.Spec.Project.Distribution}' combination is not supported")
    };
  }

  internal async Task<int> HandleAsync(CancellationToken cancellationToken = default)
  {
    Console.WriteLine($"► starting cluster '{_config.Spec.Connection.Context}'");
    await _clusterProvisioner.StartAsync(_config.Metadata.Name, cancellationToken).ConfigureAwait(false);
    Console.WriteLine("✔ cluster started");
    return 0;
  }
}
