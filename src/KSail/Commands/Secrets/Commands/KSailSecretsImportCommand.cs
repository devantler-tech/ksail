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
  internal KSailSecretsImportCommand(GlobalOptions globalOptions) : base("import", "Import a key from stdin or a file")
  {
    AddArguments();
    this.SetHandler(async (context) =>
    {
      try
      {
        var config = await KSailClusterConfigLoader.LoadWithGlobalOptionsAsync(globalOptions, context);
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
            handler = new KSailSecretsImportCommandHandler(config, key, new SOPSLocalAgeSecretManager());
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

  void AddArguments() => AddArgument(_keyArgument);
}
