using System.CommandLine;
using Devantler.SecretManager.SOPS.LocalAge;
using KSail.Commands.Secrets.Arguments;
using KSail.Commands.Secrets.Handlers;
using KSail.Options;
using KSail.Utils;

namespace KSail.Commands.Secrets.Commands;

sealed class KSailSecretsEditCommand : Command
{
  readonly ExceptionHandler _exceptionHandler = new();
  readonly PathArgument _pathArgument = new("The path to the file to edit.") { Arity = ArgumentArity.ExactlyOne };
  readonly ProjectSecretManagerOption _projectSecretManagerOption = new() { Arity = ArgumentArity.ZeroOrOne };

  internal KSailSecretsEditCommand() : base("edit", "Edit an encrypted file")
  {
    AddArgument(_pathArgument);
    AddOptions();
    this.SetHandler(async (context) =>
    {
      try
      {
        var config = await KSailClusterConfigLoader.LoadAsync().ConfigureAwait(false);
        config.UpdateConfig("Spec.Project.SecretManager", context.ParseResult.GetValueForOption(_projectSecretManagerOption));
        string path = context.ParseResult.GetValueForArgument(_pathArgument);
        var cancellationToken = context.GetCancellationToken();
        KSailSecretsEditCommandHandler handler;
        switch (config.Spec.Project.SecretManager)
        {
          default:
          case Models.Project.KSailSecretManager.None:
            _ = _exceptionHandler.HandleException(new KSailException("no secret manager configured"));
            context.ExitCode = 1;
            return;
          case Models.Project.KSailSecretManager.SOPS:
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

  void AddOptions() => AddOption(_projectSecretManagerOption);
}
