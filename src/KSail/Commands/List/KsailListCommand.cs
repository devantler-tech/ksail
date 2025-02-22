using System.CommandLine;
using KSail.Commands.List.Handlers;
using KSail.Commands.List.Options;
using KSail.Options;
using KSail.Utils;

namespace KSail.Commands.List;

sealed class KSailListCommand : Command
{
  readonly ExceptionHandler _exceptionHandler = new();
  readonly AllOption _allOption = new() { Arity = ArgumentArity.ZeroOrOne };
  internal KSailListCommand(GlobalOptions globalOptions) : base("list", "List active clusters")
  {
    AddOption(_allOption);
    this.SetHandler(async (context) =>
    {
      try
      {
        var config = await KSailClusterConfigLoader.LoadWithGlobalOptionsAsync(globalOptions, context);
        config.UpdateConfig("Spec.CLI.List.All", context.ParseResult.GetValueForOption(_allOption));
        var cancellationToken = context.GetCancellationToken();
        var handler = new KSailListCommandHandler(config);

        _ = await handler.HandleAsync(context.GetCancellationToken()).ConfigureAwait(false);
      }
      catch (Exception ex)
      {
        _ = _exceptionHandler.HandleException(ex);
        context.ExitCode = 1;
      }
    });
  }
}
