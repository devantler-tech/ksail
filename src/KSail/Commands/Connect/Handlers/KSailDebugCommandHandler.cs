using System.Diagnostics.CodeAnalysis;
using System.Globalization;
using DevantlerTech.K9sCLI;
using KSail.Models;

namespace KSail.Commands.Connect.Handlers;

[ExcludeFromCodeCoverage]
class KSailConnectCommandHandler : ICommandHandler
{
  readonly KSailCluster _config;

  internal KSailConnectCommandHandler(KSailCluster config) => _config = config;

  public async Task<int> HandleAsync(CancellationToken cancellationToken = default)
  {
    string[] args = ["--kubeconfig", _config.Spec.Connection.Kubeconfig, "--context", _config.Spec.Connection.Context];
    // TODO: Update k9s call when pseudo-terminal support is added to CLIWrap. See https://github.com/Tyrrrz/CliWrap/issues/225.
    Environment.SetEnvironmentVariable("EDITOR", _config.Spec.Project.Editor.ToString().ToLower(CultureInfo.CurrentCulture));
    int exitCode = await K9s.RunAsync(args, cancellationToken: cancellationToken).ConfigureAwait(false);
    Environment.SetEnvironmentVariable("EDITOR", null);
    return exitCode;
  }
}
