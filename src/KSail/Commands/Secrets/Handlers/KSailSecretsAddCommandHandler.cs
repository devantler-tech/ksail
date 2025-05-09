using System.CommandLine;
using Devantler.Keys.Age;
using Devantler.SecretManager.Core;

namespace KSail.Commands.Secrets.Handlers;

class KSailSecretsAddCommandHandler(ISecretManager<AgeKey> secretManager, IConsole console) : ICommandHandler
{
  readonly ISecretManager<AgeKey> _secretManager = secretManager;

  public async Task<int> HandleAsync(CancellationToken cancellationToken)
  {
    var key = await _secretManager.CreateKeyAsync(cancellationToken).ConfigureAwait(false);
    console.WriteLine(key.ToString());
    return 0;
  }
}
