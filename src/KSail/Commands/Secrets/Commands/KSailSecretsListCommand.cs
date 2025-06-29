using System.CommandLine;
using DevantlerTech.SecretManager.SOPS.LocalAge;
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

    SetAction(async (parseResult, cancellationToken) =>
    {
      try
      {
        var config = await KSailClusterConfigLoader.LoadWithoptionsAsync(parseResult).ConfigureAwait(false);

        var handler = new KSailSecretsListCommandHandler(config, new SOPSLocalAgeSecretManager(), parseResult);
        await handler.HandleAsync(cancellationToken).ConfigureAwait(false);
      }
      catch (Exception ex)
      {
        _ = _exceptionHandler.HandleException(ex);

      }
    });
  }

  void AddOptions()
  {
    Options.Add(CLIOptions.SecretManager.SOPS.ShowPrivateKeysInListingsOption);
    Options.Add(CLIOptions.SecretManager.SOPS.ShowAllKeysInListingsOption);
  }
}
