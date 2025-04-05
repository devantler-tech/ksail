using System.CommandLine.Invocation;
using Devantler.KubernetesGenerator.Core.Converters;
using Devantler.KubernetesGenerator.Core.Inspectors;
using KSail.Models;
using KSail.Models.Project.Enums;
using KSail.Options;
using YamlDotNet.Serialization;
using YamlDotNet.Serialization.NamingConventions;
using YamlDotNet.System.Text.Json;

namespace KSail.Utils;

static class KSailClusterConfigLoader
{
  static readonly IDeserializer _deserializer = new DeserializerBuilder()
      .WithTypeInspector(inner => new KubernetesTypeInspector(new SystemTextJsonTypeInspector(inner)))
      .WithTypeConverter(new IntstrIntOrStringTypeConverter())
      .WithTypeConverter(new ResourceQuantityTypeConverter())
      .WithNamingConvention(CamelCaseNamingConvention.Instance).Build();

  internal static async Task<KSailCluster> LoadWithoptionsAsync(InvocationContext context)
  {
    var config = await LoadAsync(
      context.ParseResult.GetValueForOption(CLIOptions.Project.ConfigPathOption),
      context.ParseResult.GetValueForOption(CLIOptions.Metadata.NameOption),
      context.ParseResult.GetValueForOption(CLIOptions.Project.DistributionOption)
    ).ConfigureAwait(false);
    // Metadata
    config.UpdateConfig(c => c.Metadata.Name, context.ParseResult.GetValueForOption(CLIOptions.Metadata.NameOption));

    // CNI
    // TODO: Implement CNI CLIOptions

    // Connection
    config.UpdateConfig(c => c.Spec.Connection.Context, context.ParseResult.GetValueForOption(CLIOptions.Connection.ContextOption));
    config.UpdateConfig(c => c.Spec.Connection.Kubeconfig, context.ParseResult.GetValueForOption(CLIOptions.Connection.KubeconfigOption));
    config.UpdateConfig(c => c.Spec.Connection.Timeout, context.ParseResult.GetValueForOption(CLIOptions.Connection.TimeoutOption));

    // DeploymentTool
    config.UpdateConfig(c => c.Spec.DeploymentTool.Flux.Source.Url, context.ParseResult.GetValueForOption(CLIOptions.DeploymentTool.Flux.SourceOption));

    // Distribution
    config.UpdateConfig(c => c.Spec.Distribution.ShowAllClustersInListings, context.ParseResult.GetValueForOption(CLIOptions.Distribution.ShowAllClustersInListings));

    // Generator
    config.UpdateConfig(c => c.Spec.Generator.Overwrite, context.ParseResult.GetValueForOption(CLIOptions.Generator.OverwriteOption));

    // IngressController
    // TODO: Implement IngressController CLIOptions

    // LocalRegistry
    // TODO: Implement LocalRegistry CLIOptions

    // MirrorRegistries
    // TODO: Implement MirrorRegistries CLIOptions

    // Project
    config.UpdateConfig(c => c.Spec.Project.CNI, context.ParseResult.GetValueForOption(CLIOptions.Project.CNIOption));
    config.UpdateConfig(c => c.Spec.Project.ConfigPath, context.ParseResult.GetValueForOption(CLIOptions.Project.ConfigPathOption));
    config.UpdateConfig(c => c.Spec.Project.DeploymentTool, context.ParseResult.GetValueForOption(CLIOptions.Project.DeploymentToolOption));
    config.UpdateConfig(c => c.Spec.Project.Distribution, context.ParseResult.GetValueForOption(CLIOptions.Project.DistributionOption));
    config.UpdateConfig(c => c.Spec.Project.DistributionConfigPath, context.ParseResult.GetValueForOption(CLIOptions.Project.DistributionConfigPathOption));
    config.UpdateConfig(c => c.Spec.Project.Editor, context.ParseResult.GetValueForOption(CLIOptions.Project.EditorOption));
    config.UpdateConfig(c => c.Spec.Project.Provider, context.ParseResult.GetValueForOption(CLIOptions.Project.ProviderOption));
    config.UpdateConfig(c => c.Spec.Project.KustomizationPath, context.ParseResult.GetValueForOption(CLIOptions.Project.KustomizationPathOption));
    config.UpdateConfig(c => c.Spec.Project.MirrorRegistries, context.ParseResult.GetValueForOption(CLIOptions.Project.MirrorRegistriesOption));
    config.UpdateConfig(c => c.Spec.Project.SecretManager, context.ParseResult.GetValueForOption(CLIOptions.Project.SecretManagerOption));

    // SecretManager
    config.UpdateConfig(c => c.Spec.SecretManager.SOPS.InPlace, context.ParseResult.GetValueForOption(CLIOptions.SecretManager.SOPS.InPlaceOption));
    config.UpdateConfig(c => c.Spec.SecretManager.SOPS.PublicKey, context.ParseResult.GetValueForOption(CLIOptions.SecretManager.SOPS.PublicKeyOption));
    config.UpdateConfig(c => c.Spec.SecretManager.SOPS.ShowAllKeysInListings, context.ParseResult.GetValueForOption(CLIOptions.SecretManager.SOPS.ShowAllKeysInListingsOption));
    config.UpdateConfig(c => c.Spec.SecretManager.SOPS.ShowPrivateKeysInListings, context.ParseResult.GetValueForOption(CLIOptions.SecretManager.SOPS.ShowPrivateKeysInListingsOption));

    // Validation
    config.UpdateConfig(c => c.Spec.Validation.ValidateOnUp, context.ParseResult.GetValueForOption(CLIOptions.Validation.ValidateOnUpOption));
    config.UpdateConfig(c => c.Spec.Validation.ValidateOnUpdate, context.ParseResult.GetValueForOption(CLIOptions.Validation.ValidateOnUpdateOption));
    config.UpdateConfig(c => c.Spec.Validation.ReconcileOnUp, context.ParseResult.GetValueForOption(CLIOptions.Validation.ReconcileOnUpOption));
    config.UpdateConfig(c => c.Spec.Validation.ReconcileOnUpdate, context.ParseResult.GetValueForOption(CLIOptions.Validation.ReconcileOnUpdateOption));

    // GatewayController
    // TODO: Implement GatewayController CLIOptions
    return config;
  }

  internal static async Task<KSailCluster> LoadAsync(string? configFilePath = "ksail.yaml", string? name = default, KSailDistributionType distribution = default)
  {
    // Create default KSailClusterConfig
    var ksailClusterConfig = string.IsNullOrEmpty(name) ?
      new KSailCluster(distribution: distribution) :
      new KSailCluster(name, distribution: distribution);

    // Locate KSail YAML file
    string startDirectory = Directory.GetCurrentDirectory();
    string? ksailYaml = string.IsNullOrEmpty(configFilePath) ?
      FindConfigFile(startDirectory, "ksail.yaml") :
      FindConfigFile(startDirectory, configFilePath);


    // If no KSail YAML file is found, return the default KSailClusterConfig
    if (ksailYaml == null)
    {
      return ksailClusterConfig;
    }

    // Deserialize KSail YAML file
    ksailClusterConfig = _deserializer.Deserialize<KSailCluster>(await File.ReadAllTextAsync(ksailYaml).ConfigureAwait(false));

    return ksailClusterConfig;
  }

  static string? FindConfigFile(string startDirectory, string configFilePath)
  {
    if (Path.IsPathRooted(configFilePath))
    {
      return File.Exists(configFilePath) ? configFilePath : null;
    }
    string? currentDirectory = startDirectory;
    while (currentDirectory != null)
    {
      string filePath = Path.Combine(currentDirectory, configFilePath);
      if (File.Exists(filePath))
        return filePath;
      var parentDirectory = Directory.GetParent(currentDirectory);
      currentDirectory = parentDirectory?.FullName;
    }
    return null;
  }
}
