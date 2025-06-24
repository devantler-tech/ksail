using System.CommandLine;
using KSail.Commands.Up.Handlers;
using KSail.Options;
using KSail.Utils;

namespace KSail.Commands.Up;

sealed class KSailUpCommand : Command
{
  readonly ExceptionHandler _exceptionHandler = new();
  internal KSailUpCommand() : base("up", "Create a cluster")
  {
    AddOptions();

    this.SetAction(async (parseResult, cancellationToken) =>
    {
      try
      {
        var config = await KSailClusterConfigLoader.LoadWithoptionsAsync(parseResult).ConfigureAwait(false);
        var handler = new KSailUpCommandHandler(config);
        await handler.HandleAsync(cancellationToken).ConfigureAwait(false);
      }
      catch (Exception ex)
      {
        _ = _exceptionHandler.HandleException(ex);

      }
    });
  }

  void AddOptions()
  {
    Options.Add(CLIOptions.Connection.ContextOption);
    Options.Add(CLIOptions.Connection.KubeconfigOption);
    Options.Add(CLIOptions.Connection.TimeoutOption);
    Options.Add(CLIOptions.Metadata.NameOption);
    Options.Add(CLIOptions.Project.DistributionConfigPathOption);
    Options.Add(CLIOptions.Project.KustomizationPathOption);
    Options.Add(CLIOptions.Project.ContainerEngineOption);
    Options.Add(CLIOptions.Project.DistributionOption);
    Options.Add(CLIOptions.Project.DeploymentToolOption);
    Options.Add(CLIOptions.Project.CNIOption);
    Options.Add(CLIOptions.Project.CSIOption);
    Options.Add(CLIOptions.Project.IngressControllerOption);
    Options.Add(CLIOptions.Project.GatewayControllerOption);
    Options.Add(CLIOptions.Project.MetricsServerOption);
    Options.Add(CLIOptions.Project.MirrorRegistriesOption);
    Options.Add(CLIOptions.Project.SecretManagerOption);
    Options.Add(CLIOptions.DeploymentTool.Flux.SourceOption);
    Options.Add(CLIOptions.Validation.ValidateOnUpOption);
    Options.Add(CLIOptions.Validation.ReconcileOnUpOption);
    //Options.Add(CLIOptions.LocalRegistry.LocalRegistryOption);
    //Options.Add(CLIOptions.MirrorRegistries.MirrorRegistryOption);
  }
}
