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

    this.SetHandler(async (context) =>
    {
      try
      {
        Console.WriteLine("🚀 Creating cluster");
        var config = await KSailClusterConfigLoader.LoadWithoptionsAsync(context).ConfigureAwait(false);
        Console.WriteLine();
        var handler = new KSailUpCommandHandler(config);
        context.ExitCode = await handler.HandleAsync(context.GetCancellationToken()).ConfigureAwait(false);
      }
      catch (Exception ex)
      {
        _ = _exceptionHandler.HandleException(ex);
        context.ExitCode = 1;
      }
    });
  }

  void AddOptions()
  {
    AddOption(CLIOptions.Connection.ContextOption);
    AddOption(CLIOptions.Connection.KubeconfigOption);
    AddOption(CLIOptions.Connection.TimeoutOption);
    AddOption(CLIOptions.DeploymentTool.Flux.SourceOption);
    //AddOption(CLIOptions.LocalRegistry.LocalRegistryOption);
    AddOption(CLIOptions.Metadata.NameOption);
    //AddOption(CLIOptions.MirrorRegistries.MirrorRegistryOption);
    AddOption(CLIOptions.Project.CNIOption);
    AddOption(CLIOptions.Project.DeploymentToolOption);
    AddOption(CLIOptions.Project.DistributionConfigPathOption);
    AddOption(CLIOptions.Project.DistributionOption);
    AddOption(CLIOptions.Project.ProviderOption);
    AddOption(CLIOptions.Project.KustomizationPathOption);
    AddOption(CLIOptions.Project.MirrorRegistriesOption);
    AddOption(CLIOptions.Project.SecretManagerOption);
    AddOption(CLIOptions.Validation.ValidateOnUpOption);
    AddOption(CLIOptions.Validation.ReconcileOnUpOption);
  }
}
