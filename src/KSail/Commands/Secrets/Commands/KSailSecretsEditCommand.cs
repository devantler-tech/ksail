using System.CommandLine;
using System.Diagnostics.CodeAnalysis;
using DevantlerTech.SecretManager.SOPS.LocalAge;
using KSail.Commands.Secrets.Arguments;
using KSail.Commands.Secrets.Handlers;
using KSail.Models.Project.Enums;
using KSail.Options;
using KSail.Utils;

namespace KSail.Commands.Secrets.Commands;

[ExcludeFromCodeCoverage]
sealed class KSailSecretsEditCommand : Command
{
  readonly ExceptionHandler _exceptionHandler = new();
  readonly PathArgument _pathArgument = new("The path to the file to edit.") { Arity = ArgumentArity.ExactlyOne };

  internal KSailSecretsEditCommand() : base("edit", "Edit an encrypted file")
  {
    Arguments.Add(_pathArgument);
    Options.Add(CLIOptions.Project.EditorOption);
    SetAction(async (parseResult, cancellationToken) =>
    {
      try
      {
        var config = await KSailClusterConfigLoader.LoadWithoptionsAsync(parseResult).ConfigureAwait(false);
        string path = parseResult.GetValue(_pathArgument);
        var handler = new KSailSecretsEditCommandHandler(config, path, new SOPSLocalAgeSecretManager());
        await handler.HandleAsync(cancellationToken).ConfigureAwait(false);
        Console.WriteLine();
      }
      catch (Exception ex)
      {
        _ = _exceptionHandler.HandleException(ex);

      }
    });
  }
}
