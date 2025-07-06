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
    SetAction(async (parseResult, cancellationToken) =>
    {
      try
      {
        var config = await KSailClusterConfigLoader.LoadWithoptionsAsync(parseResult).ConfigureAwait(false);
        var handler = new KSailUpdateCommandHandler(config);
        await handler.HandleAsync(cancellationToken).ConfigureAwait(false);
        return 0;
      }
      catch (Exception ex)
      {
        _ = _exceptionHandler.HandleException(ex);
        return 1;
      }
    });
  }

  void AddOptions()
  {
    Options.Add(CLIOptions.Connection.ContextOption);
    Options.Add(CLIOptions.Connection.KubeconfigOption);
    Options.Add(CLIOptions.Project.KustomizationPathOption);
    Options.Add(CLIOptions.Project.DeploymentToolOption);
    Options.Add(CLIOptions.Publication.PublishOnUpdateOption);
    Options.Add(CLIOptions.Validation.ValidateOnUpdateOption);
    Options.Add(CLIOptions.Validation.ReconcileOnUpdateOption);
  }
}
