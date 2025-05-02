using Devantler.Keys.Age;
using Devantler.SecretManager.Core;
using KSail.Models;
using KSail.Utils;

namespace KSail.Commands.Secrets.Handlers;

class KSailSecretsListCommandHandler(KSailCluster config, ISecretManager<AgeKey> secretManager) : ICommandHandler
{
  readonly KSailCluster _config = config;
  readonly ISecretManager<AgeKey> _secretManager = secretManager;

  public async Task<int> HandleAsync(CancellationToken cancellationToken)
  {
    var keys = await _secretManager.ListKeysAsync(cancellationToken).ConfigureAwait(false);

    if (!_config.Spec.SecretManager.SOPS.ShowAllKeysInListings)
    {
      var sopsConfig = await SopsConfigLoader.LoadAsync(cancellationToken: cancellationToken).ConfigureAwait(false);
      if (!keys.Any(key => sopsConfig.CreationRules.Any(rule => rule.Age == key.PublicKey)))
      {
        Console.WriteLine("► no keys found");
        return 0;
      }
      foreach (var key in keys.Where(key => sopsConfig.CreationRules.Any(rule => rule.Age == key.PublicKey)))
      {
        if (_config.Spec.SecretManager.SOPS.ShowPrivateKeysInListings)
        {
          Console.WriteLine(key);
        }
        else
        {
          Console.WriteLine(Obscure(key));
        }
        Console.WriteLine();
      }
    }
    else
    {
      if (!keys.Any())
      {
        Console.WriteLine("► no keys found");
        return 0;
      }
      foreach (var key in keys)
      {
        if (_config.Spec.SecretManager.SOPS.ShowPrivateKeysInListings)
        {
          Console.WriteLine(key);
        }
        else
        {
          Console.WriteLine(Obscure(key));
        }
        Console.WriteLine();
      }
    }
    return 0;
  }

  static string Obscure(AgeKey key)
  {
    string keyString = key.ToString();
    keyString = keyString.Replace(keyString[keyString.LastIndexOf('\n')..], "\nAGE-SECRET-KEY-" + new string('*', 59), StringComparison.Ordinal);
    return keyString;
  }
}
