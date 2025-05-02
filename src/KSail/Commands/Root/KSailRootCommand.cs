using System.CommandLine;
using System.CommandLine.Parsing;
using KSail.Commands.Connect;
using KSail.Commands.Down;
using KSail.Commands.Gen;
using KSail.Commands.Init;
using KSail.Commands.List;
using KSail.Commands.Root.Handlers;
using KSail.Commands.Run;
using KSail.Commands.Secrets;
using KSail.Commands.Start;
using KSail.Commands.Status;
using KSail.Commands.Stop;
using KSail.Commands.Up;
using KSail.Commands.Update;
using KSail.Commands.Validate;
using KSail.Utils;

namespace KSail.Commands.Root;

sealed class KSailRootCommand : RootCommand
{
  readonly ExceptionHandler _exceptionHandler = new();

  internal KSailRootCommand(IConsole console) : base("KSail is an SDK for Kubernetes. Ship k8s with ease!")
  {
    AddCommands(console);
    this.SetHandler(async (context) =>
      {
        try
        {
          var ksailRootCommandHandler = new KSailRootCommandHandler(console);
          int exitCode = await ksailRootCommandHandler.HandleAsync().ConfigureAwait(false);
          if (exitCode == 0)
          {
            exitCode = await this.InvokeAsync("--help", console).ConfigureAwait(false);
          }
          context.ExitCode = exitCode;
        }
        catch (Exception ex)
        {
          _ = _exceptionHandler.HandleException(ex);
          context.ExitCode = 1;
        }
      }
    );
  }

  void AddCommands(IConsole console)
  {
    AddCommand(new KSailInitCommand());
    AddCommand(new KSailUpCommand());
    AddCommand(new KSailUpdateCommand());
    AddCommand(new KSailStartCommand());
    AddCommand(new KSailStopCommand());
    AddCommand(new KSailDownCommand());
    AddCommand(new KSailStatusCommand());
    AddCommand(new KSailListCommand());
    AddCommand(new KSailValidateCommand());
    AddCommand(new KSailConnectCommand());
    AddCommand(new KSailGenCommand(console));
    AddCommand(new KSailSecretsCommand(console));
    AddCommand(new KSailRunCommand(console));
  }
}
