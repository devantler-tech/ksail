using System.CommandLine;
using System.Diagnostics.CodeAnalysis;
using KSail.Commands.Debug.Handlers;
using KSail.Options;
using KSail.Utils;

namespace KSail.Commands.Debug;

[ExcludeFromCodeCoverage]
sealed class KSailDebugCommand : Command
{
  readonly ExceptionHandler _exceptionHandler = new();

  internal KSailDebugCommand() : base("debug", "Debug a cluster (❤️ K9s)")
  {
    AddOptions();
    this.SetHandler(async (context) =>
    {
      try
      {
        var config = await KSailClusterConfigLoader.LoadWithoptionsAsync(context).ConfigureAwait(false);
        var handler = new KSailDebugCommandHandler(config);
        context.ExitCode = await handler.HandleAsync(context.GetCancellationToken()).ConfigureAwait(false) ? 0 : 1;
      }
      catch (Exception ex)
      {
        _ = _exceptionHandler.HandleException(ex);
        context.ExitCode = 1;
      }
    });
  }

  internal void AddOptions()
  {
    AddOption(CLIOptions.Connection.KubeconfigOption);
    AddOption(CLIOptions.Connection.ContextOption);
    AddOption(CLIOptions.Project.EditorOption);
  }
}
