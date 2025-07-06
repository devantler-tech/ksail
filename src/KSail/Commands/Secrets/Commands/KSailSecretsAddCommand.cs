using System.CommandLine;
using DevantlerTech.SecretManager.SOPS.LocalAge;
using KSail.Commands.Secrets.Handlers;
using KSail.Models.Project.Enums;
using KSail.Utils;

namespace KSail.Commands.Secrets.Commands;

sealed class KSailSecretsAddCommand : Command
{
  readonly ExceptionHandler _exceptionHandler = new();

  internal KSailSecretsAddCommand() : base("add", "Add a new encryption key")
  {
    SetAction(async (parseResult, cancellationToken) =>
    {
      try
      {
        var config = await KSailClusterConfigLoader.LoadWithoptionsAsync(parseResult).ConfigureAwait(false);
        var handler = new KSailSecretsAddCommandHandler(new SOPSLocalAgeSecretManager(), parseResult);
        await handler.HandleAsync(cancellationToken).ConfigureAwait(false);
        return 0;
      }
      catch (Exception ex)
      {
        _ = _exceptionHandler.HandleException(ex);
        return 1;
      }
    });
  }
}


