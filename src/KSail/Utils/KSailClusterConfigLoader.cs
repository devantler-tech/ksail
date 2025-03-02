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
    config.UpdateConfig("Metadata.Name", context.ParseResult.GetValueForOption(CLIOptions.Metadata.NameOption));

    // CNI
    // TODO: Implement CNI CLIOptions

    // Connection
    config.UpdateConfig("Spec.Connection.Context", context.ParseResult.GetValueForOption(CLIOptions.Connection.ContextOption));
    config.UpdateConfig("Spec.Connection.Kubeconfig", context.ParseResult.GetValueForOption(CLIOptions.Connection.KubeconfigOption));
    config.UpdateConfig("Spec.Connection.Timeout", context.ParseResult.GetValueForOption(CLIOptions.Connection.TimeoutOption));

    // DeploymentTool
    config.UpdateConfig("Spec.DeploymentTool.Flux.Source", context.ParseResult.GetValueForOption(CLIOptions.DeploymentTool.Flux.SourceOption));

    // Distribution
    config.UpdateConfig("Spec.Distribution.ShowAllClustersInListings", context.ParseResult.GetValueForOption(CLIOptions.Distribution.ShowAllClustersInListings));

    // IngressController
    // TODO: Implement IngressController CLIOptions

    // LocalRegistry
    // TODO: Implement LocalRegistry CLIOptions

    // MirrorRegistries
    // TODO: Implement MirrorRegistries CLIOptions

    // Project
    config.UpdateConfig("Spec.Project.ConfigPath", context.ParseResult.GetValueForOption(CLIOptions.Project.ConfigPathOption));
    config.UpdateConfig("Spec.Project.DeploymentTool", context.ParseResult.GetValueForOption(CLIOptions.Project.DeploymentToolOption));
    config.UpdateConfig("Spec.Project.Distribution", context.ParseResult.GetValueForOption(CLIOptions.Project.DistributionOption));
    config.UpdateConfig("Spec.Project.DistributionConfigPath", context.ParseResult.GetValueForOption(CLIOptions.Project.DistributionConfigPathOption));
    config.UpdateConfig("Spec.Project.Editor", context.ParseResult.GetValueForOption(CLIOptions.Project.EditorOption));
    config.UpdateConfig("Spec.Project.Engine", context.ParseResult.GetValueForOption(CLIOptions.Project.EngineOption));
    config.UpdateConfig("Spec.Project.KustomizationPath", context.ParseResult.GetValueForOption(CLIOptions.Project.KustomizationPathOption));
    config.UpdateConfig("Spec.Project.MirrorRegistries", context.ParseResult.GetValueForOption(CLIOptions.Project.MirrorRegistriesOption));
    config.UpdateConfig("Spec.Project.SecretManager", context.ParseResult.GetValueForOption(CLIOptions.Project.SecretManagerOption));

    // SecretManager
    config.UpdateConfig("Spec.SecretManager.SOPS.InPlace", context.ParseResult.GetValueForOption(CLIOptions.SecretManager.SOPS.InPlaceOption));
    config.UpdateConfig("Spec.SecretManager.SOPS.PublicKey", context.ParseResult.GetValueForOption(CLIOptions.SecretManager.SOPS.PublicKeyOption));
    config.UpdateConfig("Spec.SecretManager.SOPS.ShowAllKeysInListings", context.ParseResult.GetValueForOption(CLIOptions.SecretManager.SOPS.ShowAllKeysInListingsOption));
    config.UpdateConfig("Spec.SecretManager.SOPS.ShowPrivateKeysInListings", context.ParseResult.GetValueForOption(CLIOptions.SecretManager.SOPS.ShowPrivateKeysInListingsOption));

    // Validation
    config.UpdateConfig("Spec.Validation.LintOnUp", context.ParseResult.GetValueForOption(CLIOptions.Validation.LintOnUpOption));
    config.UpdateConfig("Spec.Validation.LintOnUpdate", context.ParseResult.GetValueForOption(CLIOptions.Validation.LintOnUpdateOption));
    config.UpdateConfig("Spec.Validation.ReconcileOnUp", context.ParseResult.GetValueForOption(CLIOptions.Validation.ReconcileOnUpOption));
    config.UpdateConfig("Spec.Validation.ReconcileOnUpdate", context.ParseResult.GetValueForOption(CLIOptions.Validation.ReconcileOnUpdateOption));

    // WaypointController
    // TODO: Implement WaypointController CLIOptions
    return config;
  }

  internal static async Task<KSailCluster> LoadAsync(string? configFilePath = "ksail-config.yaml", string? name = default, KSailKubernetesDistributionType distribution = default)
  {
    // Create default KSailClusterConfig
    var ksailClusterConfig = string.IsNullOrEmpty(name) ?
      new KSailCluster(distribution: distribution) :
      new KSailCluster(name, distribution: distribution);

    // Locate KSail YAML file
    string startDirectory = Directory.GetCurrentDirectory();
    string? ksailYaml = string.IsNullOrEmpty(configFilePath) ?
      FindConfigFile(startDirectory, "ksail-config.yaml") :
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
