using Devantler.Keys.Age;
using Devantler.SecretManager.SOPS.LocalAge;
using Devantler.SecretManager.SOPS.LocalAge.Models;
using Devantler.SecretManager.SOPS.LocalAge.Utils;
using KSail.Models;

namespace KSail.Commands.Init.Generators.SubGenerators;

class SOPSConfigFileGenerator
{
  SOPSLocalAgeSecretManager SOPSLocalAgeSecretManager { get; } = new();
  SOPSConfigHelper SOPSConfigHelper { get; } = new();

  internal async Task GenerateAsync(KSailCluster config, CancellationToken cancellationToken = default)
  {
    string sopsConfigPath = Path.Combine(config.Spec.Project.WorkingDirectory, ".sops.yaml");
    if (!File.Exists(sopsConfigPath) || string.IsNullOrEmpty(await File.ReadAllTextAsync(sopsConfigPath, cancellationToken).ConfigureAwait(false)))
    {
      await GenerateNewSOPSConfigFile(
        sopsConfigPath,
        config.Metadata.Name,
        await SOPSLocalAgeSecretManager.CreateKeyAsync(cancellationToken).ConfigureAwait(false),
        cancellationToken
      ).ConfigureAwait(false);
    }
    else
    {
      var sopsConfig = await SOPSConfigHelper.GetSOPSConfigAsync(sopsConfigPath, cancellationToken).ConfigureAwait(false);
      var existingCreationRule = sopsConfig.CreationRules.FirstOrDefault(cr => cr.PathRegex.Contains(config.Metadata.Name, StringComparison.OrdinalIgnoreCase));
      var ageKey = default(AgeKey);
      if (existingCreationRule is null)
      {
        ageKey = await SOPSLocalAgeSecretManager.CreateKeyAsync(cancellationToken).ConfigureAwait(false);
      }
      else
      {
        string publicKey = existingCreationRule.Age;
        ageKey = await SOPSLocalAgeSecretManager.GetKeyAsync(publicKey, cancellationToken).ConfigureAwait(false);
      }
      await GenerateUpdatedSOPSConfigFile(sopsConfigPath, config.Metadata.Name, ageKey, cancellationToken).ConfigureAwait(false);
    }
  }

  async Task GenerateNewSOPSConfigFile(string path, string clusterName, AgeKey ageKey, CancellationToken cancellationToken = default)
  {
    Console.WriteLine($"✚ generating '{path}'");
    var sopsConfig = new SOPSConfig()
    {
      CreationRules =
        [
            new()
            {
              PathRegex = @$"^k8s\/clusters\/{clusterName}\/.+\.enc\.ya?ml$",
              Age = ageKey.PublicKey
            },
          new()
          {
            PathRegex = @"^.+\.enc\.ya?ml$",
            Age = ageKey.PublicKey
          }
        ]
    };
    await SOPSConfigHelper.CreateSOPSConfigAsync(path, sopsConfig, cancellationToken: cancellationToken).ConfigureAwait(false);
  }

  async Task GenerateUpdatedSOPSConfigFile(string path, string clusterName, AgeKey ageKey, CancellationToken cancellationToken = default)
  {
    Console.WriteLine($"✚ generating and overwriting '{path}'");
    var sopsConfig = await SOPSConfigHelper.GetSOPSConfigAsync(path, cancellationToken: cancellationToken).ConfigureAwait(false);

    // Check if the creation rule already exists
    if (!sopsConfig.CreationRules.Any(cr => cr.PathRegex == @$"^k8s\/clusters\/{clusterName}\/.+\.enc\.ya?ml$"))
    {
      // Add new creation rule to the start of the list
      sopsConfig.CreationRules.Insert(0, new SOPSConfigCreationRule
      {
        PathRegex = @$"^k8s\/clusters\/{clusterName}\/.+\.enc\.ya?ml$",
        Age = ageKey.PublicKey
      });
      var lastCreationRule = sopsConfig.CreationRules.Last();
      string lastCreationRuleAge = lastCreationRule.Age;
      lastCreationRule.Age = $"{lastCreationRuleAge},{Environment.NewLine}{ageKey.PublicKey}";
      await SOPSConfigHelper.CreateSOPSConfigAsync(path, sopsConfig, true, cancellationToken).ConfigureAwait(false);
    }
  }
}
