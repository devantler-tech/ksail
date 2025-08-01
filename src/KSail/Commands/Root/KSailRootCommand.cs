using System.CommandLine;
using System.CommandLine.Help;
using System.CommandLine.Parsing;
using KSail.Commands.Connect;
using KSail.Commands.Down;
using KSail.Commands.Gen;
using KSail.Commands.Init;
using KSail.Commands.List;
using KSail.Commands.Root.Handlers;
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

  internal KSailRootCommand() : base("KSail is an SDK for Kubernetes. Ship k8s with ease!")
  {
    AddCommands();
    SetAction(async (parseResult, cancellationToken) =>
      {
        try
        {
          var ksailRootCommandHandler = new KSailRootCommandHandler(parseResult);
          await ksailRootCommandHandler.HandleAsync(cancellationToken).ConfigureAwait(false);
          if (!parseResult.CommandResult.Children.Any())
          {
            var helpResult = Parse("--help",
              new CommandLineConfiguration(this)
              {
                Output = parseResult.Configuration.Output,
                Error = parseResult.Configuration.Error
              }
            );
            _ = await helpResult.InvokeAsync(cancellationToken).ConfigureAwait(false);
          }
          return 0;
        }
        catch (Exception ex)
        {
          _ = _exceptionHandler.HandleException(ex);
          return 1;
        }
      }
    );
  }

  void AddCommands()
  {
    Subcommands.Add(new KSailInitCommand());
    Subcommands.Add(new KSailUpCommand());
    Subcommands.Add(new KSailUpdateCommand());
    Subcommands.Add(new KSailStartCommand());
    Subcommands.Add(new KSailStopCommand());
    Subcommands.Add(new KSailDownCommand());
    Subcommands.Add(new KSailStatusCommand());
    Subcommands.Add(new KSailListCommand());
    Subcommands.Add(new KSailValidateCommand());
    Subcommands.Add(new KSailConnectCommand());
    Subcommands.Add(new KSailGenCommand());
    Subcommands.Add(new KSailSecretsCommand());
  }
}
