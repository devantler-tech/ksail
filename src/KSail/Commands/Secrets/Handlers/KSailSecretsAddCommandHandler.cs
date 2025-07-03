using System.CommandLine;
using DevantlerTech.Keys.Age;
using DevantlerTech.SecretManager.Core;

namespace KSail.Commands.Secrets.Handlers;

class KSailSecretsAddCommandHandler(ISecretManager<AgeKey> secretManager, ParseResult parseResult) : ICommandHandler
{
  readonly ISecretManager<AgeKey> _secretManager = secretManager;

  public async Task HandleAsync(CancellationToken cancellationToken)
  {
    var key = await _secretManager.CreateKeyAsync(cancellationToken).ConfigureAwait(false);
    await parseResult.Configuration.Output.WriteLineAsync(key.ToString()).ConfigureAwait(false);
  }
}
