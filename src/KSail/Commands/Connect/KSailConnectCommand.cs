using System.CommandLine;
using System.Diagnostics.CodeAnalysis;
using KSail.Commands.Connect.Handlers;
using KSail.Options;
using KSail.Utils;

namespace KSail.Commands.Connect;

[ExcludeFromCodeCoverage]
sealed class KSailConnectCommand : Command
{
  readonly ExceptionHandler _exceptionHandler = new();

  internal KSailConnectCommand() : base("connect", "Connect to a cluster with K9s")
  {
    AddOptions();
    this.SetHandler(async (context) =>
    {
      try
      {
        var config = await KSailClusterConfigLoader.LoadWithoptionsAsync(context).ConfigureAwait(false);
        var handler = new KSailConnectCommandHandler(config);
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
