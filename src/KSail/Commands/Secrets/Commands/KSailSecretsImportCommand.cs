using System.CommandLine;
using Devantler.SecretManager.SOPS.LocalAge;
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
    this.SetHandler(async (context) =>
    {
      try
      {
        var config = await KSailClusterConfigLoader.LoadWithoptionsAsync(context).ConfigureAwait(false);
        string key = context.ParseResult.GetValueForArgument(_keyArgument);

        var cancellationToken = context.GetCancellationToken();
        var handler = new KSailSecretsImportCommandHandler(key, new SOPSLocalAgeSecretManager());
        context.ExitCode = await handler.HandleAsync(cancellationToken).ConfigureAwait(false);
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
