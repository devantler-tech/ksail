using System.CommandLine;
using Devantler.SecretManager.SOPS.LocalAge;
using KSail.Commands.Secrets.Arguments;
using KSail.Commands.Secrets.Handlers;
using KSail.Models.Project.Enums;
using KSail.Options;
using KSail.Utils;

namespace KSail.Commands.Secrets.Commands;

sealed class KSailSecretsEditCommand : Command
{
  readonly ExceptionHandler _exceptionHandler = new();
  readonly PathArgument _pathArgument = new("The path to the file to edit.") { Arity = ArgumentArity.ExactlyOne };

  internal KSailSecretsEditCommand() : base("edit", "Edit an encrypted file")
  {
    AddArgument(_pathArgument);
    AddOption(CLIOptions.Project.EditorOption);
    this.SetHandler(async (context) =>
    {
      try
      {
        var config = await KSailClusterConfigLoader.LoadWithoptionsAsync(context).ConfigureAwait(false);
        string path = context.ParseResult.GetValueForArgument(_pathArgument);
        var cancellationToken = context.GetCancellationToken();
        var handler = new KSailSecretsEditCommandHandler(config, path, new SOPSLocalAgeSecretManager());
        context.ExitCode = await handler.HandleAsync(cancellationToken).ConfigureAwait(false);
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
