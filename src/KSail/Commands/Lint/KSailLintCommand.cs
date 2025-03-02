using System.CommandLine;
using KSail.Commands.Lint.Handlers;
using KSail.Options;
using KSail.Utils;

namespace KSail.Commands.Lint;

sealed class KSailLintCommand : Command
{
  readonly ExceptionHandler _exceptionHandler = new();
  internal KSailLintCommand() : base(
   "lint", "Lint manifests for a cluster"
  )
  {
    AddOption(CLIOptions.Project.KustomizationPathOption);
    this.SetHandler(async (context) =>
    {
      try
      {
        var config = await KSailClusterConfigLoader.LoadWithoptionsAsync(context).ConfigureAwait(false);

        Console.WriteLine("🧹 Linting manifest files");
        var handler = new KSailLintCommandHandler(config);
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
