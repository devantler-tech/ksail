using Devantler.KubernetesProvisioner.Cluster.Core;
using Devantler.KubernetesProvisioner.Cluster.K3d;
using Devantler.KubernetesProvisioner.Cluster.Kind;
using KSail.Models;
using KSail.Models.Project;

namespace KSail.Commands.Stop.Handlers;

class KSailStopCommandHandler
{
  readonly KSailCluster _config;
  readonly IKubernetesClusterProvisioner _clusterProvisioner;

  internal KSailStopCommandHandler(KSailCluster config)
  {
    _config = config;
    _clusterProvisioner = (_config.Spec.Project.Engine, _config.Spec.Project.Distribution) switch
    {
      (KSailEngine.Docker, KSailKubernetesDistribution.Native) => new KindProvisioner(),
      (KSailEngine.Docker, KSailKubernetesDistribution.K3s) => new K3dProvisioner(),
      _ => throw new NotSupportedException($"The distribution '{_config.Spec.Project.Distribution}' is not supported.")
    };
  }

  internal async Task<int> HandleAsync(CancellationToken cancellationToken = default)
  {
    await _clusterProvisioner.StopAsync(_config.Metadata.Name, cancellationToken).ConfigureAwait(false);
    return 0;
  }
}
