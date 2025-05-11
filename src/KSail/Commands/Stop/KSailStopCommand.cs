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
    this.SetHandler(async (context) =>
    {
      var config = await KSailClusterConfigLoader.LoadWithoptionsAsync(context).ConfigureAwait(false);

      var handler = new KSailStopCommandHandler(config);
      try
      {
        _ = await handler.HandleAsync(context.GetCancellationToken()).ConfigureAwait(false);
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
    AddOption(CLIOptions.Metadata.NameOption);
    AddOption(CLIOptions.Project.ContainerEngineOption);
    AddOption(CLIOptions.Project.DistributionOption);
  }
}
