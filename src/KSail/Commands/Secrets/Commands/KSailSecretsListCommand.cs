using System.CommandLine;
using Devantler.SecretManager.SOPS.LocalAge;
using KSail.Commands.Secrets.Handlers;
using KSail.Models.Project.Enums;
using KSail.Options;
using KSail.Utils;

namespace KSail.Commands.Secrets.Commands;

sealed class KSailSecretsListCommand : Command
{
  readonly ExceptionHandler _exceptionHandler = new();
  internal KSailSecretsListCommand() : base("list", "List keys")
  {
    AddOptions();

    this.SetHandler(async (context) =>
    {
      try
      {
        var config = await KSailClusterConfigLoader.LoadWithoptionsAsync(context).ConfigureAwait(false);

        var cancellationToken = context.GetCancellationToken();
        var handler = new KSailSecretsListCommandHandler(config, new SOPSLocalAgeSecretManager());
        context.ExitCode = await handler.HandleAsync(cancellationToken).ConfigureAwait(false) ? 0 : 1;
      }
      catch (Exception ex)
      {
        _ = _exceptionHandler.HandleException(ex);
        context.ExitCode = 1;
      }
    });
  }

  void AddOptions()
  {
    AddOption(CLIOptions.SecretManager.SOPS.ShowPrivateKeysInListingsOption);
    AddOption(CLIOptions.SecretManager.SOPS.ShowAllKeysInListingsOption);
  }
}
