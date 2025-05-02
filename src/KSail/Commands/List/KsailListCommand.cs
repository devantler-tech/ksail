using System.CommandLine;
using KSail.Commands.List.Handlers;
using KSail.Options;
using KSail.Utils;

namespace KSail.Commands.List;

sealed class KSailListCommand : Command
{
  readonly ExceptionHandler _exceptionHandler = new();
  internal KSailListCommand() : base("list", "List active clusters")
  {
    AddOptions();
    this.SetHandler(async (context) =>
    {
      try
      {
        var config = await KSailClusterConfigLoader.LoadWithoptionsAsync(context).ConfigureAwait(false);
        var cancellationToken = context.GetCancellationToken();
        var handler = new KSailListCommandHandler(config);
        context.ExitCode = await handler.HandleAsync(context.GetCancellationToken()).ConfigureAwait(false);
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
    AddOption(CLIOptions.Project.ContainerEngineOption);
    AddOption(CLIOptions.Project.DistributionOption);
    AddOption(CLIOptions.Distribution.ShowAllClustersInListings);
  }
}
