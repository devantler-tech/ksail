using System.CommandLine;
using DevantlerTech.Keys.Age;
using DevantlerTech.SecretManager.Core;

namespace KSail.Commands.Secrets.Handlers;

class KSailSecretsAddCommandHandler(ISecretManager<AgeKey> secretManager) : ICommandHandler
{
  readonly ISecretManager<AgeKey> _secretManager = secretManager;

  public async Task HandleAsync(CancellationToken cancellationToken)
  {
    var key = await _secretManager.CreateKeyAsync(cancellationToken).ConfigureAwait(false);
    Console.WriteLine(key.ToString());
    return 0;
  }
}
