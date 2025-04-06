using Devantler.KubernetesProvisioner.Cluster.K3d;
using Devantler.KubernetesProvisioner.Cluster.Kind;
using Devantler.KubernetesProvisioner.Resources.Native;
using KSail.Models;
using KSail.Models.Project.Enums;

namespace KSail.Commands.Status.Handlers;

sealed class KSailStatusCommandHandler(KSailCluster config)
{
  readonly KSailCluster _config = config;

  internal async Task<bool> HandleAsync(CancellationToken cancellationToken = default)
  {
    var (ExitCode, _) = await Devantler.KubectlCLI.Kubectl.RunAsync(
      ["get", "--raw", "/livez?verbose", "--kubeconfig", _config.Spec.Connection.Kubeconfig, "--context", _config.Spec.Connection.Context],
      cancellationToken: cancellationToken
    ).ConfigureAwait(false);
    return ExitCode == 0;
  }
}
