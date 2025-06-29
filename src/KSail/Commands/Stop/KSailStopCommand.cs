using System.CommandLine;
using KSail.Commands.Stop.Handlers;
using KSail.Options;
using KSail.Utils;

namespace KSail.Commands.Stop;

sealed class KSailStopCommand : Command
{
  readonly ExceptionHandler _exceptionHandler = new();

  internal KSailStopCommand() : base("stop", "Stop a cluster")
  {
    AddOptions();
    SetAction(async (parseResult, cancellationToken) =>
    {
      var config = await KSailClusterConfigLoader.LoadWithoptionsAsync(parseResult).ConfigureAwait(false);

      var handler = new KSailStopCommandHandler(config);
      try
      {
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
    Options.Add(CLIOptions.Metadata.NameOption);
    Options.Add(CLIOptions.Project.ContainerEngineOption);
    Options.Add(CLIOptions.Project.DistributionOption);
  }
}
