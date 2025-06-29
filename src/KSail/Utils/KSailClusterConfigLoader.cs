using System.CommandLine;
using System.CommandLine.Invocation;
using DevantlerTech.KubernetesGenerator.Core.Converters;
using DevantlerTech.KubernetesGenerator.Core.Inspectors;
using KSail.Models;
using KSail.Models.LocalRegistry;
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

  internal static async Task<KSailCluster> LoadWithoptionsAsync(ParseResult parseResult, string path = "./")
  {
    Console.WriteLine("⏳ Loading configuration...");
    string configFilePath = Path.Combine(path, parseResult.GetValue(CLIOptions.Project.ConfigPathOption) ?? "ksail.yaml");
    var config = await LoadAsync(
      configFilePath,
      parseResult.GetValue(CLIOptions.Metadata.NameOption),
      parseResult.GetValue(CLIOptions.Project.DistributionOption) ?? KSailDistributionType.Kind
    ).ConfigureAwait(false);
    // Metadata
    config.UpdateConfig(c => c.Metadata.Name, parseResult.GetValue(CLIOptions.Metadata.NameOption));

    // Connection
    config.UpdateConfig(c => c.Spec.Connection.Context, parseResult.GetValue(CLIOptions.Connection.ContextOption));
    config.UpdateConfig(c => c.Spec.Connection.Kubeconfig, parseResult.GetValue(CLIOptions.Connection.KubeconfigOption));
    config.UpdateConfig(c => c.Spec.Connection.Timeout, parseResult.GetValue(CLIOptions.Connection.TimeoutOption));

    // Project
    config.UpdateConfig(c => c.Spec.Project.ConfigPath, parseResult.GetValue(CLIOptions.Project.ConfigPathOption));
    config.UpdateConfig(c => c.Spec.Project.DistributionConfigPath, parseResult.GetValue(CLIOptions.Project.DistributionConfigPathOption));
    config.UpdateConfig(c => c.Spec.Project.KustomizationPath, parseResult.GetValue(CLIOptions.Project.KustomizationPathOption));
    config.UpdateConfig(c => c.Spec.Project.ContainerEngine, parseResult.GetValue(CLIOptions.Project.ContainerEngineOption));
    config.UpdateConfig(c => c.Spec.Project.Distribution, parseResult.GetValue(CLIOptions.Project.DistributionOption));
    config.UpdateConfig(c => c.Spec.Project.DeploymentTool, parseResult.GetValue(CLIOptions.Project.DeploymentToolOption));
    config.UpdateConfig(c => c.Spec.Project.CNI, parseResult.GetValue(CLIOptions.Project.CNIOption));
    config.UpdateConfig(c => c.Spec.Project.CSI, parseResult.GetValue(CLIOptions.Project.CSIOption));
    config.UpdateConfig(c => c.Spec.Project.IngressController, parseResult.GetValue(CLIOptions.Project.IngressControllerOption));
    config.UpdateConfig(c => c.Spec.Project.GatewayController, parseResult.GetValue(CLIOptions.Project.GatewayControllerOption));
    config.UpdateConfig(c => c.Spec.Project.MetricsServer, parseResult.GetValue(CLIOptions.Project.MetricsServerOption));
    config.UpdateConfig(c => c.Spec.Project.SecretManager, parseResult.GetValue(CLIOptions.Project.SecretManagerOption));
    config.UpdateConfig(c => c.Spec.Project.MirrorRegistries, parseResult.GetValue(CLIOptions.Project.MirrorRegistriesOption));
    config.UpdateConfig(c => c.Spec.Project.Editor, parseResult.GetValue(CLIOptions.Project.EditorOption));

    // Distribution
    config.UpdateConfig(c => c.Spec.Distribution.ShowAllClustersInListings, parseResult.GetValue(CLIOptions.Distribution.ShowAllClustersInListings));

    // CNI
    // TODO: Implement CNI CLIOptions

    // CSI
    // TODO: Implement CSI CLIOptions

    // IngressController
    // TODO: Implement IngressController CLIOptions

    // GatewayController
    // TODO: Implement GatewayController CLIOptions

    // DeploymentTool
    config.UpdateConfig(c => c.Spec.DeploymentTool.Flux.Source.Url, parseResult.GetValue(CLIOptions.DeploymentTool.Flux.SourceOption));

    // SecretManager
    config.UpdateConfig(c => c.Spec.SecretManager.SOPS.InPlace, parseResult.GetValue(CLIOptions.SecretManager.SOPS.InPlaceOption));
    config.UpdateConfig(c => c.Spec.SecretManager.SOPS.PublicKey, parseResult.GetValue(CLIOptions.SecretManager.SOPS.PublicKeyOption));
    config.UpdateConfig(c => c.Spec.SecretManager.SOPS.ShowAllKeysInListings, parseResult.GetValue(CLIOptions.SecretManager.SOPS.ShowAllKeysInListingsOption));
    config.UpdateConfig(c => c.Spec.SecretManager.SOPS.ShowPrivateKeysInListings, parseResult.GetValue(CLIOptions.SecretManager.SOPS.ShowPrivateKeysInListingsOption));

    // LocalRegistry
    // TODO: Implement LocalRegistry CLIOptions

    // MirrorRegistries
    // TODO: Implement MirrorRegistries CLIOptions
    for (int i = 0; i < config.Spec.MirrorRegistries.Count(); i++)
    {
      var mirrorRegistry = config.Spec.MirrorRegistries.ElementAt(i);
      if (mirrorRegistry.Provider == KSailContainerEngineType.Docker)
      {
        config.Spec.MirrorRegistries.ElementAt(i).Provider = parseResult.GetValue(CLIOptions.Project.ContainerEngineOption) ?? KSailContainerEngineType.Docker;
      }
    }

    // Generator
    config.UpdateConfig(c => c.Spec.Generator.Overwrite, parseResult.GetValue(CLIOptions.Generator.OverwriteOption));

    // Publication
    config.UpdateConfig(c => c.Spec.Publication.PublishOnUpdate, parseResult.GetValue(CLIOptions.Publication.PublishOnUpdateOption));

    // Validation
    config.UpdateConfig(c => c.Spec.Validation.ValidateOnUp, parseResult.GetValue(CLIOptions.Validation.ValidateOnUpOption));
    config.UpdateConfig(c => c.Spec.Validation.ValidateOnUpdate, parseResult.GetValue(CLIOptions.Validation.ValidateOnUpdateOption));
    config.UpdateConfig(c => c.Spec.Validation.ReconcileOnUp, parseResult.GetValue(CLIOptions.Validation.ReconcileOnUpOption));
    config.UpdateConfig(c => c.Spec.Validation.ReconcileOnUpdate, parseResult.GetValue(CLIOptions.Validation.ReconcileOnUpdateOption));
    config.UpdateConfig(c => c.Spec.Validation.Verbose, parseResult.GetValue(CLIOptions.Validation.VerboseOption));

    Console.WriteLine($"✔ configuration loaded.");
    Console.WriteLine();
    return config;
  }

  internal static async Task<KSailCluster> LoadAsync(string configFilePath, string? name = default, KSailDistributionType distribution = default)
  {
    // Create default KSailClusterConfig
    var ksailClusterConfig = string.IsNullOrEmpty(name) ?
      new KSailCluster(distribution: distribution) :
      new KSailCluster(name, distribution: distribution);

    // Locate KSail YAML file
    string startDirectory = Directory.GetCurrentDirectory();
    string? ksailYaml = FindConfigFile(startDirectory, configFilePath);


    // If no KSail YAML file is found, return the default KSailClusterConfig
    if (ksailYaml == null)
    {
      Console.WriteLine($"► '{configFilePath}' not found. Using default configuration.");
      return ksailClusterConfig;
    }
    Console.WriteLine($"► '{configFilePath}' found.");

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
