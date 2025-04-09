using System.Diagnostics.CodeAnalysis;
using System.Globalization;
using Devantler.K9sCLI;
using KSail.Models;

namespace KSail.Commands.Debug.Handlers;

[ExcludeFromCodeCoverage]
class KSailDebugCommandHandler
{
  readonly KSailCluster _config;

  internal KSailDebugCommandHandler(KSailCluster config) => _config = config;

  internal async Task<bool> HandleAsync(CancellationToken cancellationToken = default)
  {
    string[] args = ["--kubeconfig", _config.Spec.Connection.Kubeconfig, "--context", _config.Spec.Connection.Context];
    // TODO: Update k9s call when pseudo-terminal support is added to CLIWrap. See https://github.com/Tyrrrz/CliWrap/issues/225.
    Environment.SetEnvironmentVariable("EDITOR", _config.Spec.Project.Editor.ToString().ToLower(CultureInfo.CurrentCulture));
    int exitCode = await K9s.RunAsync(args, cancellationToken: cancellationToken).ConfigureAwait(false);
    Environment.SetEnvironmentVariable("EDITOR", null);
    return exitCode == 0;
  }
}
