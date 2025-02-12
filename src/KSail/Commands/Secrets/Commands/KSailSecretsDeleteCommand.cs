using System.CommandLine;
using KSail.Options;
using KSail.Utils;

namespace KSail.Commands.Secrets.Commands;

sealed class KSailSecretsDeleteCommand : Command
{
  readonly ExceptionHandler _exceptionHandler = new();
  readonly PublicKeyArgument _publicKeyArgument = new() { Arity = ArgumentArity.ExactlyOne };
  readonly ProjectSecretManagerOption _projectSecretManagerOption = new() { Arity = ArgumentArity.ZeroOrOne };
  internal KSailSecretsDeleteCommand() : base("del", "Delete an existing encryption key")
  {
    AddArgument(_publicKeyArgument);
    AddOption(_projectSecretManagerOption);
    this.SetHandler(async (context) =>
    {
      try
      {
        var config = await KSailClusterConfigLoader.LoadAsync().ConfigureAwait(false);
        config.UpdateConfig("Spec.Project.SecretManager", context.ParseResult.GetValueForOption(_projectSecretManagerOption));
        string publicKey = context.ParseResult.GetValueForArgument(_publicKeyArgument);
        var cancellationToken = context.GetCancellationToken();
        var handler = new KSailSecretsDeleteCommandHandler(publicKey);

        if (config.Spec.Project.SecretManager == Models.Project.KSailSecretManager.None)
        {
          _ = _exceptionHandler.HandleException(new KSailException("no secret manager configured"));
          context.ExitCode = 1;
          return;
        }

        Console.WriteLine($"🔑 Removing an existing encryption key with '{config.Spec.Project.SecretManager}'");
        context.ExitCode = await handler.HandleAsync(context.GetCancellationToken()).ConfigureAwait(false);
        Console.WriteLine();
      }
      catch (OperationCanceledException ex)
      {
        _ = _exceptionHandler.HandleException(ex);
        context.ExitCode = 1;
      }
    });
  }
}


