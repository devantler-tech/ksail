using System.CommandLine;
using Devantler.SecretManager.SOPS.LocalAge;
using KSail.Commands.Secrets.Arguments;
using KSail.Commands.Secrets.Handlers;
using KSail.Models.Project.Enums;
using KSail.Utils;

namespace KSail.Commands.Secrets.Commands;

sealed class KSailSecretsEditCommand : Command
{
  readonly ExceptionHandler _exceptionHandler = new();
  readonly PathArgument _pathArgument = new("The path to the file to edit.") { Arity = ArgumentArity.ExactlyOne };

  internal KSailSecretsEditCommand() : base("edit", "Edit an encrypted file")
  {
    AddArgument(_pathArgument);
    this.SetHandler(async (context) =>
    {
      try
      {
        var config = await KSailClusterConfigLoader.LoadWithoptionsAsync(context).ConfigureAwait(false);
        string path = context.ParseResult.GetValueForArgument(_pathArgument);
        var cancellationToken = context.GetCancellationToken();
        KSailSecretsEditCommandHandler handler;
        switch (config.Spec.Project.SecretManager)
        {
          default:
          case KSailSecretManagerType.None:
            _ = _exceptionHandler.HandleException(new KSailException("no secret manager configured"));
            context.ExitCode = 1;
            return;
          case KSailSecretManagerType.SOPS:
            handler = new KSailSecretsEditCommandHandler(config, path, new SOPSLocalAgeSecretManager());
            break;
        }
        context.ExitCode = await handler.HandleAsync(context.GetCancellationToken()).ConfigureAwait(false);
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
