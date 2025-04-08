using System.CommandLine;
using KSail.Commands.List.Handlers;
using KSail.Commands.Status.Handlers;
using KSail.Options;
using KSail.Utils;

namespace KSail.Commands.Status;

sealed class KSailStatusCommand : Command
{
  readonly ExceptionHandler _exceptionHandler = new();
  internal KSailStatusCommand() : base("status", "Show the status of a cluster")
  {
    AddOptions();
    this.SetHandler(async (context) =>
    {
      try
      {
        var config = await KSailClusterConfigLoader.LoadWithoptionsAsync(context).ConfigureAwait(false);
        var cancellationToken = context.GetCancellationToken();
        var handler = new KSailStatusCommandHandler(config);
        context.ExitCode = await handler.HandleAsync(cancellationToken).ConfigureAwait(false) ? 0 : 1;
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
    AddOption(CLIOptions.Validation.VerboseOption);
  }
}
