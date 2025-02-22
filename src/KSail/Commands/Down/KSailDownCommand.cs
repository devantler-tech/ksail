using System.CommandLine;
using KSail.Commands.Down.Handlers;
using KSail.Options;
using KSail.Utils;

namespace KSail.Commands.Down;

sealed class KSailDownCommand : Command
{
  readonly ExceptionHandler _exceptionHandler = new();
  internal KSailDownCommand(GlobalOptions globalOptions) : base("down", "Destroy a cluster")
  {
    this.SetHandler(async (context) =>
    {
      try
      {
        var config = await KSailClusterConfigLoader.LoadWithGlobalOptionsAsync(globalOptions, context);

        var handler = new KSailDownCommandHandler(config);
        context.ExitCode = await handler.HandleAsync(context.GetCancellationToken()).ConfigureAwait(false) ? 0 : 1;
      }
      catch (Exception ex)
      {
        _ = _exceptionHandler.HandleException(ex);
        context.ExitCode = 1;
      }
    });
  }
}
