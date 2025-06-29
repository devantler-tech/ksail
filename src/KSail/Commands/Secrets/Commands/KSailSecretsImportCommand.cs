using System.CommandLine;
using DevantlerTech.SecretManager.SOPS.LocalAge;
using KSail.Commands.Secrets.Handlers;
using KSail.Models.Project.Enums;
using KSail.Utils;

namespace KSail.Commands.Secrets.Commands;

sealed class KSailSecretsImportCommand : Command
{
  readonly ExceptionHandler _exceptionHandler = new();
  readonly KeyArgument _keyArgument = new("The encryption key to import") { Arity = ArgumentArity.ExactlyOne };
  internal KSailSecretsImportCommand() : base("import", "Import a key from stdin or a file")
  {
    AddArguments();
    SetAction(async (parseResult, cancellationToken) =>
    {
      try
      {
        var config = await KSailClusterConfigLoader.LoadWithoptionsAsync(parseResult).ConfigureAwait(false);
        string key = parseResult.GetValue(_keyArgument) ?? throw new KSailException("Key argument is required.");

        var handler = new KSailSecretsImportCommandHandler(key, new SOPSLocalAgeSecretManager());
        await handler.HandleAsync(cancellationToken).ConfigureAwait(false);
      }
      catch (Exception ex)
      {
        _ = _exceptionHandler.HandleException(ex);

      }
    });
  }

  void AddArguments() => Arguments.Add(_keyArgument);
}
