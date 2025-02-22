using System.CommandLine;
using KSail.Commands.Init.Handlers;
using KSail.Commands.Init.Options;
using KSail.Options;
using KSail.Utils;

namespace KSail.Commands.Init;

sealed class KSailInitCommand : Command
{
  readonly ExceptionHandler _exceptionHandler = new();
  readonly FluxDeploymentToolPostBuildVariablesOption _fluxDeploymentToolPostBuildVariablesOption = new() { Arity = ArgumentArity.ZeroOrOne };
  readonly KustomizeTemplateFlowsOption _kustomizeTemplateKustomizationsOption = new() { Arity = ArgumentArity.ZeroOrMore, AllowMultipleArgumentsPerToken = true };
  readonly KustomizeTemplateHooksOption _kustomizeTemplateKustomizationHooksOption = new() { Arity = ArgumentArity.ZeroOrMore, AllowMultipleArgumentsPerToken = true };

  public KSailInitCommand(GlobalOptions globalOptions) : base("init", "Initialize a cluster")
  {
    AddOptions();

    this.SetHandler(async (context) =>
    {
      try
      {
        var config = await KSailClusterConfigLoader.LoadWithGlobalOptionsAsync(globalOptions, context);
        config.UpdateConfig("Spec.FluxDeploymentTool.PostBuildVariables", context.ParseResult.GetValueForOption(_fluxDeploymentToolPostBuildVariablesOption));
        config.UpdateConfig("Spec.KustomizeTemplate.Flows", context.ParseResult.GetValueForOption(_kustomizeTemplateKustomizationsOption));
        config.UpdateConfig("Spec.KustomizeTemplate.Hooks", context.ParseResult.GetValueForOption(_kustomizeTemplateKustomizationHooksOption));
        var handler = new KSailInitCommandHandler(config);
        Console.WriteLine($"📁 Initializing new cluster '{config.Metadata.Name}' in '{config.Spec.Project.WorkingDirectory}' with the '{config.Spec.Project.Template}' template.");
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
    AddOption(_fluxDeploymentToolPostBuildVariablesOption);
    AddOption(_kustomizeTemplateKustomizationHooksOption);
    AddOption(_kustomizeTemplateKustomizationsOption);
  }
}
