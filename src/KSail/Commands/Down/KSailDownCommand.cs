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
    SetAction(async (parseResult, cancellationToken) =>
    {
      try
      {
        var config = await KSailClusterConfigLoader.LoadWithoptionsAsync(parseResult).ConfigureAwait(false);

        var handler = new KSailDownCommandHandler(config);
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
    //AddOptions(CLIOptions.MirrorRegistries.MirrorRegistryOption);
    Options.Add(CLIOptions.DeploymentTool.Flux.SourceOption);
    Options.Add(CLIOptions.Metadata.NameOption);
    Options.Add(CLIOptions.Project.DistributionOption);
    Options.Add(CLIOptions.Project.ContainerEngineOption);
    Options.Add(CLIOptions.Project.MirrorRegistriesOption);
  }
}
