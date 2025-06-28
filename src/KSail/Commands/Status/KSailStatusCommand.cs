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
    SetAction(async (parseResult, cancellationToken) =>
    {
      try
      {
        var config = await KSailClusterConfigLoader.LoadWithoptionsAsync(parseResult).ConfigureAwait(false);
        var handler = new KSailStatusCommandHandler(config, parseResult);
        await handler.HandleAsync(cancellationToken).ConfigureAwait(false);
      }
      catch (Exception ex)
      {
        _ = _exceptionHandler.HandleException(ex);

      }
    });
  }

  internal void AddOptions()
  {
    Options.Add(CLIOptions.Connection.KubeconfigOption);
    Options.Add(CLIOptions.Connection.ContextOption);
    Options.Add(CLIOptions.Validation.VerboseOption);
  }
}
