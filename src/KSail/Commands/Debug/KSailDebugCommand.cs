using System.CommandLine;
using KSail.Commands.Debug.Handlers;
using KSail.Options;
using KSail.Utils;

namespace KSail.Commands.Debug;

sealed class KSailDebugCommand : Command
{
  readonly ExceptionHandler _exceptionHandler = new();

  internal KSailDebugCommand(GlobalOptions globalOptions) : base("debug", "Debug a cluster (❤️ K9s)")
  {
    this.SetHandler(async (context) =>
    {
      try
      {
        var config = await KSailClusterConfigLoader.LoadWithGlobalOptionsAsync(globalOptions, context);
        var handler = new KSailDebugCommandHandler(config);
        context.ExitCode = await handler.HandleAsync(context.GetCancellationToken()).ConfigureAwait(false) ? 0 : 1;
        Console.WriteLine();
      }
      catch (Exception ex)
      {
        _ = _exceptionHandler.HandleException(ex);
        context.ExitCode = 1;
      }
    });
  }
}
