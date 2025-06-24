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

    this.SetAction(async (parseResult, cancellationToken) =>
    {
      try
      {
        string outputPath = parseResult.CommandResult.GetValue(_outputPathOption) ?? "./";
        var config = await KSailClusterConfigLoader.LoadWithoptionsAsync(parseResult).ConfigureAwait(false);
        var handler = new KSailInitCommandHandler(outputPath, config);
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
    Options.Add(_outputPathOption);
    Options.Add(CLIOptions.Metadata.NameOption);
    Options.Add(CLIOptions.Project.ConfigPathOption);
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
    Options.Add(CLIOptions.Project.EditorOption);
    Options.Add(CLIOptions.Generator.OverwriteOption);
  }
}
