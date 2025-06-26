using DevantlerTech.Keys.Age;
using DevantlerTech.SecretManager.Core;
using KSail.Models;

namespace KSail.Commands.Secrets.Handlers;

class KSailSecretsExportCommandHandler(string publicKey, string outputPath, ISecretManager<AgeKey> secretManager) : ICommandHandler
{
  readonly string _publicKey = publicKey;
  readonly string _outputPath = outputPath;
  readonly ISecretManager<AgeKey> _secretManager = secretManager;

  public async Task HandleAsync(CancellationToken cancellationToken)
  {
    Console.WriteLine($"► exporting '{_publicKey}' from SOPS to '{_outputPath}'");
    var key = await _secretManager.GetKeyAsync(_publicKey, cancellationToken).ConfigureAwait(false);
    await File.WriteAllTextAsync(_outputPath, key.ToString(), cancellationToken).ConfigureAwait(false);
    Console.WriteLine("✔ key exported");
  }
}
