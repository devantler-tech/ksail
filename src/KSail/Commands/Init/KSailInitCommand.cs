using System.CommandLine;
using KSail.Commands.Init.Handlers;
using KSail.Options;
using KSail.Utils;

namespace KSail.Commands.Init;

sealed class KSailInitCommand : Command
{
  readonly GenericPathOption _outputPathOption = new("./", ["-o", "--output"])
  {
    Description = "Output directory for the project files. [default: ./]"
  };
  readonly ExceptionHandler _exceptionHandler = new();

  public KSailInitCommand() : base("init", "Initialize a new project")
  {
    AddOptions();

    this.SetHandler(async (context) =>
    {
      try
      {
        string outputPath = context.ParseResult.CommandResult.GetValueForOption(_outputPathOption) ?? "./";
        var config = await KSailClusterConfigLoader.LoadWithoptionsAsync(context).ConfigureAwait(false);
        var handler = new KSailInitCommandHandler(outputPath, config);
        Console.WriteLine($"📁 Initializing new project '{config.Metadata.Name}'");
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
    AddOption(_outputPathOption);
    AddOption(CLIOptions.Generator.OverwriteOption);
    AddOption(CLIOptions.Metadata.NameOption);
    AddOption(CLIOptions.Project.ConfigPathOption);
    AddOption(CLIOptions.Project.KustomizationPathOption);
    AddOption(CLIOptions.Project.ProviderOption);
    AddOption(CLIOptions.Project.DistributionOption);
    AddOption(CLIOptions.Project.DistributionConfigPathOption);
    AddOption(CLIOptions.Project.DeploymentToolOption);
    AddOption(CLIOptions.Project.CNIOption);
    AddOption(CLIOptions.Project.MirrorRegistriesOption);
    AddOption(CLIOptions.Project.SecretManagerOption);
  }
}
