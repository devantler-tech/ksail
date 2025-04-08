using System.CommandLine;
using Devantler.Keys.Age;
using Devantler.SecretManager.Core;

namespace KSail.Commands.Secrets.Handlers;

class KSailSecretsAddCommandHandler(ISecretManager<AgeKey> secretManager, IConsole? console = default)
{
  readonly ISecretManager<AgeKey> _secretManager = secretManager;

  internal async Task<int> HandleAsync(CancellationToken cancellationToken)
  {
    var key = await _secretManager.CreateKeyAsync(cancellationToken).ConfigureAwait(false);
    if (console is not null)
    {
      console.WriteLine(key.ToString());
    }
    else
    {
      Console.WriteLine(key);
    }
    return 0;
  }
}
