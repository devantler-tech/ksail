using System.CommandLine;
using KSail.Commands.Update.Handlers;
using KSail.Options;
using KSail.Utils;

namespace KSail.Commands.Update;

sealed class KSailUpdateCommand : Command
{
  readonly ExceptionHandler _exceptionHandler = new();
  internal KSailUpdateCommand() : base(
    "update",
    "Update a cluster"
  )
  {
    AddOptions();
    this.SetHandler(async (context) =>
    {
      try
      {
        var config = await KSailClusterConfigLoader.LoadWithoptionsAsync(context).ConfigureAwait(false);
        var handler = new KSailUpdateCommandHandler(config);
        context.ExitCode = await handler.HandleAsync(context.GetCancellationToken()).ConfigureAwait(false) ? 0 : 1;
      }
      catch (Exception ex)
      {
        _ = _exceptionHandler.HandleException(ex);
        context.ExitCode = 1;
      }
    });
  }

  void AddOptions()
  {
    AddOption(CLIOptions.Project.KustomizationPathOption);
    AddOption(CLIOptions.Validation.LintOnUpdateOption);
    AddOption(CLIOptions.Validation.ReconcileOnUpdateOption);
  }
}
