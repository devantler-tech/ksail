using System.CommandLine;
using DevantlerTech.SecretManager.SOPS.LocalAge;
using KSail.Commands.Secrets.Arguments;
using KSail.Commands.Secrets.Handlers;
using KSail.Models.Project.Enums;
using KSail.Utils;

namespace KSail.Commands.Secrets.Commands;

sealed class KSailSecretsRemoveCommand : Command
{
  readonly ExceptionHandler _exceptionHandler = new();
  readonly PublicKeyArgument _publicKeyArgument = new("Public key matching existing encryption key") { Arity = ArgumentArity.ExactlyOne };
  internal KSailSecretsRemoveCommand() : base("rm", "Remove an existing encryption key")
  {
    Arguments.Add(_publicKeyArgument);
    SetAction(async (parseResult, cancellationToken) =>
    {
      try
      {
        var config = await KSailClusterConfigLoader.LoadWithoptionsAsync(parseResult).ConfigureAwait(false);
        string publicKey = parseResult.GetValue(_publicKeyArgument) ?? throw new KSailException("Public key argument is required.");
        var handler = new KSailSecretsRemoveCommandHandler(publicKey, new SOPSLocalAgeSecretManager());
        await handler.HandleAsync(cancellationToken).ConfigureAwait(false);
        return 0;
      }
      catch (Exception ex)
      {
        _ = _exceptionHandler.HandleException(ex);
        return 1;
      }
    });
  }
}


