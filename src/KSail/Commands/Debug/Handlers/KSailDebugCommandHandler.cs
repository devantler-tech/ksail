using System.Globalization;
using Devantler.K9sCLI;
using KSail.Models;

namespace KSail.Commands.Debug.Handlers;

class KSailDebugCommandHandler
{
  readonly KSailCluster _config;

  internal KSailDebugCommandHandler(KSailCluster config) => _config = config;

  internal async Task<bool> HandleAsync(CancellationToken cancellationToken = default)
  {
    string[] args = [];
    // TODO: Update k9s call when pseudo-terminal support is added to CLIWrap. See https://github.com/Tyrrrz/CliWrap/issues/225.
    Environment.SetEnvironmentVariable("EDITOR", _config.Spec.Project.Editor.ToString().ToLower(CultureInfo.CurrentCulture));
    var exitCode = await K9s.RunAsync(args, cancellationToken: cancellationToken).ConfigureAwait(false);
    Environment.SetEnvironmentVariable("EDITOR", null);
    return exitCode == 0;
  }
}
