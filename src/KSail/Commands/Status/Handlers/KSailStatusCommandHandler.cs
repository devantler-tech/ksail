using System.CommandLine;
using DevantlerTech.KubernetesProvisioner.Cluster.K3d;
using DevantlerTech.KubernetesProvisioner.Cluster.Kind;
using DevantlerTech.KubernetesProvisioner.Resources.Native;
using KSail.Models;
using KSail.Models.Project.Enums;

namespace KSail.Commands.Status.Handlers;

sealed class KSailStatusCommandHandler(KSailCluster config, ParseResult parseResult) : ICommandHandler
{
  readonly KSailCluster _config = config;

  public async Task HandleAsync(CancellationToken cancellationToken = default)
  {
    var (_, LiveCheckMessage) = await DevantlerTech.KubectlCLI.Kubectl.RunAsync(
      ["get", "--raw", $"/livez{(_config.Spec.Validation.Verbose ? "?verbose" : "")}", "--kubeconfig", _config.Spec.Connection.Kubeconfig, "--context", _config.Spec.Connection.Context],
      silent: true,
      cancellationToken: cancellationToken
    ).ConfigureAwait(false);
    var (_, ReadyCheckMessage) = await DevantlerTech.KubectlCLI.Kubectl.RunAsync(
      ["get", "--raw", $"/readyz{(_config.Spec.Validation.Verbose ? "?verbose" : "")}", "--kubeconfig", _config.Spec.Connection.Kubeconfig, "--context", _config.Spec.Connection.Context],
      silent: true,
      cancellationToken: cancellationToken
    ).ConfigureAwait(false);
    if (_config.Spec.Validation.Verbose)
    {
      await parseResult.InvocationConfiguration.Output.WriteLineAsync(LiveCheckMessage).ConfigureAwait(false);
      await parseResult.InvocationConfiguration.Output.WriteLineAsync(ReadyCheckMessage).ConfigureAwait(false);
    }
    else
    {
      await parseResult.InvocationConfiguration.Output.WriteLineAsync($"Live: {LiveCheckMessage}").ConfigureAwait(false);
      await parseResult.InvocationConfiguration.Output.WriteLineAsync($"Ready: {ReadyCheckMessage}").ConfigureAwait(false);
    }
  }
}
