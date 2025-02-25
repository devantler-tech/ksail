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
        var config = await KSailClusterConfigLoader.LoadWithoptionsAsync(context);
        string publicKey = context.ParseResult.GetValueForArgument(_publicKeyArgument);
        var cancellationToken = context.GetCancellationToken();
        KSailSecretsRemoveCommandHandler handler;
        switch (config.Spec.Project.SecretManager)
        {
          default:
          case KSailSecretManagerType.None:
            _ = _exceptionHandler.HandleException(new KSailException("no secret manager configured"));
            context.ExitCode = 1;
            return;
          case KSailSecretManagerType.SOPS:
            handler = new KSailSecretsRemoveCommandHandler(config, publicKey, new SOPSLocalAgeSecretManager());
            break;
        }

        context.ExitCode = await handler.HandleAsync(context.GetCancellationToken()).ConfigureAwait(false);
      }
      catch (Exception ex)
      {
        _ = _exceptionHandler.HandleException(ex);
        context.ExitCode = 1;
      }
    });
  }
}


