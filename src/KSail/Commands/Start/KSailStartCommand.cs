using System.CommandLine;
using KSail.Commands.Start.Handlers;
using KSail.Options;
using KSail.Utils;

namespace KSail.Commands.Start;

sealed class KSailStartCommand : Command
{
  readonly ExceptionHandler _exceptionHandler = new();

  internal KSailStartCommand(GlobalOptions globalOptions) : base("start", "Start a cluster")
  {
    this.SetHandler(async (context) =>
    {
      try
      {
        var config = await KSailClusterConfigLoader.LoadWithGlobalOptionsAsync(globalOptions, context);

        Console.WriteLine($"► starting cluster '{config.Spec.Connection.Context}'");
        var handler = new KSailStartCommandHandler(config);
        context.ExitCode = await handler.HandleAsync(context.GetCancellationToken()).ConfigureAwait(false);
        if (context.ExitCode == 0)
        {
          Console.WriteLine("✔ cluster started");
        }
        else
        {
          throw new KSailException("cluster could not be started");
        }
      }
      catch (Exception ex)
      {
        _ = _exceptionHandler.HandleException(ex);
        context.ExitCode = 1;
      }
    });
  }
}
