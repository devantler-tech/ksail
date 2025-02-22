using System.CommandLine;
using KSail.Commands.Lint.Handlers;
using KSail.Options;
using KSail.Utils;

namespace KSail.Commands.Lint;

sealed class KSailLintCommand : Command
{
  readonly ExceptionHandler _exceptionHandler = new();
  internal KSailLintCommand(GlobalOptions globalOptions) : base(
   "lint", "Lint manifests for a cluster"
  )
  {
    this.SetHandler(async (context) =>
    {
      try
      {
        var config = await KSailClusterConfigLoader.LoadWithGlobalOptionsAsync(globalOptions, context);

        Console.WriteLine("🧹 Linting manifest files");
        var handler = new KSailLintCommandHandler();
        context.ExitCode = await handler.HandleAsync(config, context.GetCancellationToken()).ConfigureAwait(false) ? 0 : 1;
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
