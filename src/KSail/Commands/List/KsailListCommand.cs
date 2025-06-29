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
    SetAction(async (parseResult, cancellationToken) =>
    {
      try
      {
        var config = await KSailClusterConfigLoader.LoadWithoptionsAsync(parseResult).ConfigureAwait(false);
        var handler = new KSailListCommandHandler(config, parseResult);
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
    Options.Add(CLIOptions.Project.ContainerEngineOption);
    Options.Add(CLIOptions.Project.DistributionOption);
    Options.Add(CLIOptions.Distribution.ShowAllClustersInListings);
  }
}
