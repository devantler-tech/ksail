using System.CommandLine;
using KSail.Commands.Down.Handlers;
using KSail.Options;
using KSail.Utils;

namespace KSail.Commands.Down;

sealed class KSailDownCommand : Command
{
  readonly ExceptionHandler _exceptionHandler = new();
  internal KSailDownCommand() : base("down", "Destroy a cluster")
  {
    AddOptions();
    this.SetHandler(async (context) =>
    {
      try
      {
        var config = await KSailClusterConfigLoader.LoadWithoptionsAsync(context).ConfigureAwait(false);

        var handler = new KSailDownCommandHandler(config);
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
    //AddOptions(CLIOptions.MirrorRegistries.MirrorRegistryOption);
    AddOption(CLIOptions.DeploymentTool.Flux.SourceOption);
    AddOption(CLIOptions.Metadata.NameOption);
    AddOption(CLIOptions.Project.DistributionOption);
    AddOption(CLIOptions.Project.ContainerEngineOption);
    AddOption(CLIOptions.Project.MirrorRegistriesOption);
  }
}
