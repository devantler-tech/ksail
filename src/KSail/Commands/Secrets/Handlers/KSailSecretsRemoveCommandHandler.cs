using System.CommandLine;
using DevantlerTech.Keys.Age;
using DevantlerTech.SecretManager.Core;
using KSail.Models;

namespace KSail.Commands.Secrets.Handlers;

class KSailSecretsRemoveCommandHandler(string publicKey, ISecretManager<AgeKey> secretManager) : ICommandHandler
{
  readonly string _publicKey = publicKey;
  readonly ISecretManager<AgeKey> _secretManager = secretManager;

  public async Task HandleAsync(CancellationToken cancellationToken)
  {
    Console.WriteLine($"► removing '{_publicKey}' from SOPS key file");
    _ = await _secretManager.DeleteKeyAsync(_publicKey, cancellationToken).ConfigureAwait(false);
    Console.WriteLine($"✔ key removed");
  }
}
