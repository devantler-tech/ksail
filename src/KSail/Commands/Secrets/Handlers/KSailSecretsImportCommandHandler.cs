using Devantler.Keys.Age;
using Devantler.SecretManager.Core;
using KSail.Models;

namespace KSail.Commands.Secrets.Handlers;

class KSailSecretsImportCommandHandler(string key, ISecretManager<AgeKey> secretManager) : ICommandHandler
{
  readonly string _key = key;
  readonly ISecretManager<AgeKey> _secretManager = secretManager;

  public async Task<int> HandleAsync(CancellationToken cancellationToken)
  {
    Console.WriteLine($"► importing '{_key}' to SOPS");
    string key = _key;
    if (File.Exists(key))
    {
      key = await File.ReadAllTextAsync(key, cancellationToken).ConfigureAwait(false);
    }
    var ageKey = new AgeKey(key.Trim());
    _ = await _secretManager.ImportKeyAsync(ageKey, cancellationToken).ConfigureAwait(false);
    Console.WriteLine("✔ key imported");
    return 0;
  }
}
