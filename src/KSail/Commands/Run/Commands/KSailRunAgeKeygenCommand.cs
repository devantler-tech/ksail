using System.CommandLine;
using Devantler.SecretManager.SOPS.LocalAge;
using KSail.Commands.Run.Arguments;
using KSail.Commands.Run.Handlers;
using KSail.Commands.Secrets.Handlers;
using KSail.Models.Project.Enums;
using KSail.Utils;

namespace KSail.Commands.Run.Commands;

sealed class KSailRunAgeKeygenCommand : Command
{
  readonly ExceptionHandler _exceptionHandler = new();
  readonly Argument<string[]> _argsArgument = new CLIArguments();

  internal KSailRunAgeKeygenCommand() : base("age-keygen", "Run 'age-keygen' command")
  {
    AddArguments();

    this.SetHandler(async (context) =>
    {
      try
      {
        var cancellationToken = context.GetCancellationToken();
        string[] args = context.ParseResult.GetValueForArgument(_argsArgument);

        var handler = new KSailRunAgeKeygenCommandHandler(args);
        context.ExitCode = await handler.HandleAsync(cancellationToken).ConfigureAwait(false);
      }
      catch (Exception ex)
      {
        _ = _exceptionHandler.HandleException(ex);
        context.ExitCode = 1;
      }
    });
  }

  void AddArguments() => AddArgument(_argsArgument);
}
