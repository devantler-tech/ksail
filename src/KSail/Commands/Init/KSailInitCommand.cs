using System.CommandLine;
using KSail.Commands.Init.Handlers;
using KSail.Options;
using KSail.Utils;

namespace KSail.Commands.Init;

sealed class KSailInitCommand : Command
{
  readonly ExceptionHandler _exceptionHandler = new();

  public KSailInitCommand() : base("init", "Initialize a cluster")
  {
    AddOptions();

    this.SetHandler(async (context) =>
    {
      try
      {
        var config = await KSailClusterConfigLoader.LoadWithoptionsAsync(context).ConfigureAwait(false);
        var handler = new KSailInitCommandHandler(config);
        Console.WriteLine($"📁 Initializing new cluster '{config.Metadata.Name}'");
        context.ExitCode = await handler.HandleAsync(context.GetCancellationToken()).ConfigureAwait(false);
        Console.WriteLine();
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
    AddOption(CLIOptions.Metadata.NameOption);
    AddOption(CLIOptions.Project.DistributionConfigPathOption);
    AddOption(CLIOptions.Project.DistributionOption);
    AddOption(CLIOptions.Project.EngineOption);
    AddOption(CLIOptions.Project.KustomizationPathOption);
    AddOption(CLIOptions.Project.MirrorRegistriesOption);
    AddOption(CLIOptions.Project.SecretManagerOption);
  }
}
