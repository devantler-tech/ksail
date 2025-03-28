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
    this.SetHandler(async (context) =>
    {
      try
      {
        var config = await KSailClusterConfigLoader.LoadWithoptionsAsync(context).ConfigureAwait(false);
        var handler = new KSailStartCommandHandler(config);
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
    AddOption(CLIOptions.Connection.ContextOption);
    AddOption(CLIOptions.Metadata.NameOption);
    AddOption(CLIOptions.Project.DistributionOption);
    AddOption(CLIOptions.Project.ProviderOption);
  }
}
