using System.CommandLine;
using Devantler.SecretManager.SOPS.LocalAge;
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
    AddArgument(_publicKeyArgument);
    this.SetHandler(async (context) =>
    {
      try
      {
        var config = await KSailClusterConfigLoader.LoadWithoptionsAsync(context).ConfigureAwait(false);
        string publicKey = context.ParseResult.GetValueForArgument(_publicKeyArgument);
        var cancellationToken = context.GetCancellationToken();
        var handler = new KSailSecretsRemoveCommandHandler(publicKey, new SOPSLocalAgeSecretManager());
        context.ExitCode = await handler.HandleAsync(cancellationToken).ConfigureAwait(false);
      }
      catch (Exception ex)
      {
        _ = _exceptionHandler.HandleException(ex);
        context.ExitCode = 1;
      }
    });
  }
}


