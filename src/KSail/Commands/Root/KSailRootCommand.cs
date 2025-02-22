using System.CommandLine;
using System.CommandLine.Parsing;
using KSail.Commands.Debug;
using KSail.Commands.Down;
using KSail.Commands.Gen;
using KSail.Commands.Init;
using KSail.Commands.Lint;
using KSail.Commands.List;
using KSail.Commands.Root.Handlers;
using KSail.Commands.Secrets;
using KSail.Commands.Start;
using KSail.Commands.Stop;
using KSail.Commands.Up;
using KSail.Commands.Update;
using KSail.Options;
using KSail.Utils;

namespace KSail.Commands.Root;

sealed class KSailRootCommand : RootCommand
{
  readonly ExceptionHandler _exceptionHandler = new();
  readonly GlobalOptions _globalOptions = new();
  internal KSailRootCommand(IConsole console) : base("KSail is an SDK for Kubernetes. Ship k8s with ease!")
  {
    AddGlobalOptions();
    AddCommands(console);
    this.SetHandler(async (context) =>
      {
        try
        {
          bool exitCode = KSailRootCommandHandler.Handle(console) && await this.InvokeAsync("--help", console).ConfigureAwait(false) == 0;
          context.ExitCode = exitCode ? 0 : 1;
        }
        catch (Exception ex)
        {
          _ = _exceptionHandler.HandleException(ex);
          context.ExitCode = 1;
        }
      }
    );
  }

  internal void AddGlobalOptions()
  {
    foreach (var option in _globalOptions.Options)
    {
      AddGlobalOption(option);
    }
  }

  void AddCommands(IConsole console)
  {
    AddCommand(new KSailUpCommand(_globalOptions));
    AddCommand(new KSailDownCommand(_globalOptions));
    AddCommand(new KSailUpdateCommand(_globalOptions));
    AddCommand(new KSailStartCommand(_globalOptions));
    AddCommand(new KSailStopCommand(_globalOptions));
    AddCommand(new KSailInitCommand(_globalOptions));
    AddCommand(new KSailLintCommand(_globalOptions));
    AddCommand(new KSailListCommand(_globalOptions));
    AddCommand(new KSailDebugCommand(_globalOptions));
    AddCommand(new KSailGenCommand(console));
    AddCommand(new KSailSecretsCommand(_globalOptions, console));
  }
}
