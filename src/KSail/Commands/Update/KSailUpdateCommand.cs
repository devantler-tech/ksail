using System.CommandLine;
using KSail.Commands.Update.Handlers;
using KSail.Options;
using KSail.Utils;

namespace KSail.Commands.Update;

sealed class KSailUpdateCommand : Command
{
  readonly ExceptionHandler _exceptionHandler = new();
  readonly LintOption _lintOption = new() { Arity = ArgumentArity.ZeroOrOne };
  readonly ReconcileOption _reconcileOption = new() { Arity = ArgumentArity.ZeroOrOne };
  internal KSailUpdateCommand(GlobalOptions globalOptions) : base(
    "update",
    "Update a cluster"
  )
  {
    AddOptions();

    this.SetHandler(async (context) =>
    {
      try
      {
        var config = await KSailClusterConfigLoader.LoadWithGlobalOptionsAsync(globalOptions, context);
        config.UpdateConfig("Spec.CLI.Update.Lint", context.ParseResult.GetValueForOption(_lintOption));
        config.UpdateConfig("Spec.CLI.Update.Reconcile", context.ParseResult.GetValueForOption(_reconcileOption));

        var handler = new KSailUpdateCommandHandler(config);
        context.ExitCode = await handler.HandleAsync(context.GetCancellationToken()).ConfigureAwait(false) ? 0 : 1;
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
    AddOption(_lintOption);
    AddOption(_reconcileOption);
  }
}
