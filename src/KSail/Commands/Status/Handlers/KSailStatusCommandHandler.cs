using DevantlerTech.KubernetesProvisioner.Cluster.K3d;
using DevantlerTech.KubernetesProvisioner.Cluster.Kind;
using DevantlerTech.KubernetesProvisioner.Resources.Native;
using KSail.Models;
using KSail.Models.Project.Enums;

namespace KSail.Commands.Status.Handlers;

sealed class KSailStatusCommandHandler(KSailCluster config) : ICommandHandler
{
  readonly KSailCluster _config = config;

  public async Task<int> HandleAsync(CancellationToken cancellationToken = default)
  {
    var (LiveCheckExitCode, LiveCheckMessage) = await DevantlerTech.KubectlCLI.Kubectl.RunAsync(
      ["get", "--raw", $"/livez{(_config.Spec.Validation.Verbose ? "?verbose" : "")}", "--kubeconfig", _config.Spec.Connection.Kubeconfig, "--context", _config.Spec.Connection.Context],
      silent: true,
      cancellationToken: cancellationToken
    ).ConfigureAwait(false);
    var (ReadyCheckExitCode, ReadyCheckMessage) = await DevantlerTech.KubectlCLI.Kubectl.RunAsync(
      ["get", "--raw", $"/readyz{(_config.Spec.Validation.Verbose ? "?verbose" : "")}", "--kubeconfig", _config.Spec.Connection.Kubeconfig, "--context", _config.Spec.Connection.Context],
      silent: true,
      cancellationToken: cancellationToken
    ).ConfigureAwait(false);
    if (_config.Spec.Validation.Verbose)
    {
      Console.WriteLine($"{LiveCheckMessage}");
      Console.WriteLine($"{ReadyCheckMessage}");
    }
    else
    {
      Console.WriteLine($"Live: {LiveCheckMessage}");
      Console.WriteLine($"Ready: {ReadyCheckMessage}");
    }
    return (LiveCheckExitCode == 0 && ReadyCheckExitCode == 0) ? 0 : 1;
  }
}
