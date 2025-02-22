using System.CommandLine;
using KSail.Commands.Up.Handlers;
using KSail.Commands.Up.Options;
using KSail.Options;
using KSail.Utils;

namespace KSail.Commands.Up;

sealed class KSailUpCommand : Command
{
  readonly ExceptionHandler _exceptionHandler = new();
  readonly FluxDeploymentToolSourceUrlOption _fluxDeploymentToolSourceUrlOption = new() { Arity = ArgumentArity.ZeroOrOne };
  readonly LintOption _cliUpLintOption = new() { Arity = ArgumentArity.ZeroOrOne };
  readonly ReconcileOption _cliUpReconcileOption = new() { Arity = ArgumentArity.ZeroOrOne };
  internal KSailUpCommand(GlobalOptions globalOptions) : base("up", "Create a cluster")
  {
    AddOptions();

    this.SetHandler(async (context) =>
    {
      try
      {
        var config = await KSailClusterConfigLoader.LoadWithGlobalOptionsAsync(globalOptions, context);

        config.UpdateConfig("Spec.KustomizeTemplate.Root", $"k8s/clusters/{config.Metadata.Name}/flux-system");
        config.UpdateConfig("Spec.FluxDeploymentTool.Source.Url", context.ParseResult.GetValueForOption(_fluxDeploymentToolSourceUrlOption));
        config.UpdateConfig("Spec.CLI.Up.Lint", context.ParseResult.GetValueForOption(_cliUpLintOption));
        config.UpdateConfig("Spec.CLI.Up.Reconcile", context.ParseResult.GetValueForOption(_cliUpReconcileOption));

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
    AddOption(_fluxDeploymentToolSourceUrlOption);
    AddOption(_cliUpLintOption);
    AddOption(_cliUpReconcileOption);
  }
}
