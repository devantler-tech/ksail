using System.CommandLine;
using Devantler.SecretManager.SOPS.LocalAge;
using KSail.Commands.Secrets.Handlers;
using KSail.Options;
using KSail.Utils;

namespace KSail.Commands.Secrets.Commands;

sealed class KSailSecretsImportCommand : Command
{
  readonly ExceptionHandler _exceptionHandler = new();
  readonly KeyArgument _keyArgument = new("The encryption key to import") { Arity = ArgumentArity.ExactlyOne };
  readonly ProjectSecretManagerOption _projectSecretManagerOption = new() { Arity = ArgumentArity.ZeroOrOne };
  internal KSailSecretsImportCommand() : base("import", "Import a key from stdin or a file")
  {
    AddArguments();
    AddOptions();
    this.SetHandler(async (context) =>
    {
      try
      {
        var config = await KSailClusterConfigLoader.LoadAsync().ConfigureAwait(false);
        config.UpdateConfig("Spec.Project.SecretManager", context.ParseResult.GetValueForOption(_projectSecretManagerOption));
        string key = context.ParseResult.GetValueForArgument(_keyArgument);

        var cancellationToken = context.GetCancellationToken();
        KSailSecretsImportCommandHandler handler;
        switch (config.Spec.Project.SecretManager)
        {
          default:
          case Models.Project.KSailSecretManager.None:
            _ = _exceptionHandler.HandleException(new KSailException("no secret manager configured"));
            context.ExitCode = 1;
            return;
          case Models.Project.KSailSecretManager.SOPS:
            handler = new KSailSecretsImportCommandHandler(key, new SOPSLocalAgeSecretManager());
            break;
        }
        Console.WriteLine($"🔑 Importing '{key}' to '{config.Spec.Project.SecretManager}'.");
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

  void AddArguments() => AddArgument(_keyArgument);
  void AddOptions() => AddOption(_projectSecretManagerOption);
}
