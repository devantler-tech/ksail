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
    if (!_config.Spec.Validation.Verbose)
    {
      Console.Write("Live: ");
    }
    var (LiveCheckExitCode, _) = await Devantler.KubectlCLI.Kubectl.RunAsync(
      ["get", "--raw", $"/livez{(_config.Spec.Validation.Verbose ? "?verbose" : "")}", "--kubeconfig", _config.Spec.Connection.Kubeconfig, "--context", _config.Spec.Connection.Context],
      cancellationToken: cancellationToken
    ).ConfigureAwait(false);
    if (!_config.Spec.Validation.Verbose)
    {
      Console.WriteLine();
      Console.Write("Ready: ");
    }
    var (ReadyCheckExitCode, _) = await Devantler.KubectlCLI.Kubectl.RunAsync(
      ["get", "--raw", $"/readyz{(_config.Spec.Validation.Verbose ? "?verbose" : "")}", "--kubeconfig", _config.Spec.Connection.Kubeconfig, "--context", _config.Spec.Connection.Context],
      cancellationToken: cancellationToken
    ).ConfigureAwait(false);
    if (!_config.Spec.Validation.Verbose)
    {
      Console.WriteLine();
    }
    return LiveCheckExitCode == 0 && ReadyCheckExitCode == 0;
  }
}
