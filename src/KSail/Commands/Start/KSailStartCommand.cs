using System.CommandLine;
using KSail.Commands.Start.Handlers;
using KSail.Options;
using KSail.Utils;

namespace KSail.Commands.Start;

sealed class KSailStartCommand : Command
{
  readonly ExceptionHandler _exceptionHandler = new();

  internal KSailStartCommand() : base("start", "Start a cluster")
  {
    AddOptions();
    SetAction(async (parseResult, cancellationToken) =>
    {
      try
      {
        var config = await KSailClusterConfigLoader.LoadWithoptionsAsync(parseResult).ConfigureAwait(false);
        var handler = new KSailStartCommandHandler(config);
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
    Options.Add(CLIOptions.Connection.ContextOption);
    Options.Add(CLIOptions.Metadata.NameOption);
    Options.Add(CLIOptions.Project.ContainerEngineOption);
    Options.Add(CLIOptions.Project.DistributionOption);
  }
}
