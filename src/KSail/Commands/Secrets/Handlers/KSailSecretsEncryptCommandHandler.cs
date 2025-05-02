using Devantler.Keys.Age;
using Devantler.SecretManager.Core;
using KSail.Models;

namespace KSail.Commands.Secrets.Handlers;

class KSailSecretsEncryptCommandHandler(KSailCluster config, string path, string? output, ISecretManager<AgeKey> secretManager) : ICommandHandler
{
  readonly string _path = path;
  readonly string? _output = output;
  readonly ISecretManager<AgeKey> _secretManager = secretManager;

  public async Task<int> HandleAsync(CancellationToken cancellationToken)
  {
    string encrypted = await _secretManager.EncryptAsync(_path, config.Spec.SecretManager.SOPS.PublicKey, cancellationToken).ConfigureAwait(false);
    if (config.Spec.SecretManager.SOPS.InPlace)
    {
      await File.WriteAllTextAsync(_path, encrypted, cancellationToken).ConfigureAwait(false);
    }
    if (!string.IsNullOrEmpty(_output))
    {
      await File.WriteAllTextAsync(_output, encrypted, cancellationToken).ConfigureAwait(false);
    }

    return 0;
  }
}
