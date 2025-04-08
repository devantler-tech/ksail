using System.CommandLine;
using Devantler.SecretManager.SOPS.LocalAge;
using KSail.Commands.Secrets.Handlers;
using KSail.Models.Project.Enums;
using KSail.Utils;

namespace KSail.Commands.Secrets.Commands;

sealed class KSailSecretsAddCommand : Command
{
  readonly ExceptionHandler _exceptionHandler = new();

  internal KSailSecretsAddCommand(IConsole? console = default) : base("add", "Add a new encryption key")
  {
    this.SetHandler(async (context) =>
    {
      try
      {
        var config = await KSailClusterConfigLoader.LoadWithoptionsAsync(context).ConfigureAwait(false);
        var cancellationToken = context.GetCancellationToken();
        var handler = new KSailSecretsAddCommandHandler(new SOPSLocalAgeSecretManager(), console);
        context.ExitCode = await handler.HandleAsync(cancellationToken).ConfigureAwait(false);
      }
      catch (Exception ex)
      {
        _ = _exceptionHandler.HandleException(ex);
        context.ExitCode = 1;
      }
    });
  }
}


