using System.CommandLine;
using DevantlerTech.Keys.Age;
using DevantlerTech.SecretManager.Core;
using KSail.Models;
using KSail.Utils;

namespace KSail.Commands.Secrets.Handlers;

class KSailSecretsListCommandHandler(KSailCluster config, ISecretManager<AgeKey> secretManager, ParseResult parseResult) : ICommandHandler
{
  readonly KSailCluster _config = config;
  readonly ISecretManager<AgeKey> _secretManager = secretManager;

  public async Task HandleAsync(CancellationToken cancellationToken)
  {
    var keys = await _secretManager.ListKeysAsync(cancellationToken).ConfigureAwait(false);

    if (!_config.Spec.SecretManager.SOPS.ShowAllKeysInListings)
    {
      var sopsConfig = await SopsConfigLoader.LoadAsync(cancellationToken: cancellationToken).ConfigureAwait(false);
      if (!keys.Any(key => sopsConfig.CreationRules.Any(rule => rule.Age == key.PublicKey)))
      {
        Console.WriteLine("► no keys found");
        return;
      }
      foreach (var key in keys.Where(key => sopsConfig.CreationRules.Any(rule => rule.Age == key.PublicKey)))
      {
        if (_config.Spec.SecretManager.SOPS.ShowPrivateKeysInListings)
        {
          await parseResult.Configuration.Output.WriteLineAsync(key.ToString()).ConfigureAwait(false);
        }
        else
        {
          await parseResult.Configuration.Output.WriteLineAsync(Obscure(key)).ConfigureAwait(false);
        }
        Console.WriteLine();
      }
    }
    else
    {
      if (!keys.Any())
      {
        Console.WriteLine("► no keys found");
        return;
      }
      foreach (var key in keys)
      {
        if (_config.Spec.SecretManager.SOPS.ShowPrivateKeysInListings)
        {
          await parseResult.Configuration.Output.WriteLineAsync(key.ToString()).ConfigureAwait(false);
        }
        else
        {
          await parseResult.Configuration.Output.WriteLineAsync(Obscure(key)).ConfigureAwait(false);
        }
        Console.WriteLine();
      }
    }
  }

  static string Obscure(AgeKey key)
  {
    string keyString = key.ToString();
    keyString = keyString.Replace(keyString[keyString.LastIndexOf('\n')..], "\nAGE-SECRET-KEY-" + new string('*', 59), StringComparison.Ordinal);
    return keyString;
  }
}
