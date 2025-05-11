using System.CommandLine.Invocation;
using Devantler.KubernetesGenerator.Core.Converters;
using Devantler.KubernetesGenerator.Core.Inspectors;
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

  internal static async Task<KSailCluster> LoadWithoptionsAsync(InvocationContext context, string path = "./")
  {
    Console.WriteLine("⏳ Loading configuration...");
    string configFilePath = Path.Combine(path, context.ParseResult.GetValueForOption(CLIOptions.Project.ConfigPathOption) ?? "ksail.yaml");
    var config = await LoadAsync(
      configFilePath,
      context.ParseResult.GetValueForOption(CLIOptions.Metadata.NameOption),
      context.ParseResult.GetValueForOption(CLIOptions.Project.DistributionOption) ?? KSailDistributionType.Kind
    ).ConfigureAwait(false);
    // Metadata
    config.UpdateConfig(c => c.Metadata.Name, context.ParseResult.GetValueForOption(CLIOptions.Metadata.NameOption));

    // Connection
    config.UpdateConfig(c => c.Spec.Connection.Context, context.ParseResult.GetValueForOption(CLIOptions.Connection.ContextOption));
    config.UpdateConfig(c => c.Spec.Connection.Kubeconfig, context.ParseResult.GetValueForOption(CLIOptions.Connection.KubeconfigOption));
    config.UpdateConfig(c => c.Spec.Connection.Timeout, context.ParseResult.GetValueForOption(CLIOptions.Connection.TimeoutOption));

    // Project
    config.UpdateConfig(c => c.Spec.Project.ConfigPath, context.ParseResult.GetValueForOption(CLIOptions.Project.ConfigPathOption));
    config.UpdateConfig(c => c.Spec.Project.DistributionConfigPath, context.ParseResult.GetValueForOption(CLIOptions.Project.DistributionConfigPathOption));
    config.UpdateConfig(c => c.Spec.Project.KustomizationPath, context.ParseResult.GetValueForOption(CLIOptions.Project.KustomizationPathOption));
    config.UpdateConfig(c => c.Spec.Project.ContainerEngine, context.ParseResult.GetValueForOption(CLIOptions.Project.ContainerEngineOption));
    config.UpdateConfig(c => c.Spec.Project.Distribution, context.ParseResult.GetValueForOption(CLIOptions.Project.DistributionOption));
    config.UpdateConfig(c => c.Spec.Project.DeploymentTool, context.ParseResult.GetValueForOption(CLIOptions.Project.DeploymentToolOption));
    config.UpdateConfig(c => c.Spec.Project.CNI, context.ParseResult.GetValueForOption(CLIOptions.Project.CNIOption));
    config.UpdateConfig(c => c.Spec.Project.CSI, context.ParseResult.GetValueForOption(CLIOptions.Project.CSIOption));
    config.UpdateConfig(c => c.Spec.Project.IngressController, context.ParseResult.GetValueForOption(CLIOptions.Project.IngressControllerOption));
    config.UpdateConfig(c => c.Spec.Project.GatewayController, context.ParseResult.GetValueForOption(CLIOptions.Project.GatewayControllerOption));
    config.UpdateConfig(c => c.Spec.Project.MetricsServer, context.ParseResult.GetValueForOption(CLIOptions.Project.MetricsServerOption));
    config.UpdateConfig(c => c.Spec.Project.SecretManager, context.ParseResult.GetValueForOption(CLIOptions.Project.SecretManagerOption));
    config.UpdateConfig(c => c.Spec.Project.MirrorRegistries, context.ParseResult.GetValueForOption(CLIOptions.Project.MirrorRegistriesOption));
    config.UpdateConfig(c => c.Spec.Project.Editor, context.ParseResult.GetValueForOption(CLIOptions.Project.EditorOption));

    // Distribution
    config.UpdateConfig(c => c.Spec.Distribution.ShowAllClustersInListings, context.ParseResult.GetValueForOption(CLIOptions.Distribution.ShowAllClustersInListings));

    // CNI
    // TODO: Implement CNI CLIOptions

    // CSI
    // TODO: Implement CSI CLIOptions

    // IngressController
    // TODO: Implement IngressController CLIOptions

    // GatewayController
    // TODO: Implement GatewayController CLIOptions

    // DeploymentTool
    config.UpdateConfig(c => c.Spec.DeploymentTool.Flux.Source.Url, context.ParseResult.GetValueForOption(CLIOptions.DeploymentTool.Flux.SourceOption));

    // SecretManager
    config.UpdateConfig(c => c.Spec.SecretManager.SOPS.InPlace, context.ParseResult.GetValueForOption(CLIOptions.SecretManager.SOPS.InPlaceOption));
    config.UpdateConfig(c => c.Spec.SecretManager.SOPS.PublicKey, context.ParseResult.GetValueForOption(CLIOptions.SecretManager.SOPS.PublicKeyOption));
    config.UpdateConfig(c => c.Spec.SecretManager.SOPS.ShowAllKeysInListings, context.ParseResult.GetValueForOption(CLIOptions.SecretManager.SOPS.ShowAllKeysInListingsOption));
    config.UpdateConfig(c => c.Spec.SecretManager.SOPS.ShowPrivateKeysInListings, context.ParseResult.GetValueForOption(CLIOptions.SecretManager.SOPS.ShowPrivateKeysInListingsOption));

    // LocalRegistry
    // TODO: Implement LocalRegistry CLIOptions

    // MirrorRegistries
    // TODO: Implement MirrorRegistries CLIOptions
    for (int i = 0; i < config.Spec.MirrorRegistries.Count(); i++)
    {
      var mirrorRegistry = config.Spec.MirrorRegistries.ElementAt(i);
      if (mirrorRegistry.Provider == KSailContainerEngineType.Docker)
      {
        config.Spec.MirrorRegistries.ElementAt(i).Provider = context.ParseResult.GetValueForOption(CLIOptions.Project.ContainerEngineOption) ?? KSailContainerEngineType.Docker;
      }
    }

    // Generator
    config.UpdateConfig(c => c.Spec.Generator.Overwrite, context.ParseResult.GetValueForOption(CLIOptions.Generator.OverwriteOption));

    // Publication
    config.UpdateConfig(c => c.Spec.Publication.PublishOnUpdate, context.ParseResult.GetValueForOption(CLIOptions.Publication.PublishOnUpdateOption));

    // Validation
    config.UpdateConfig(c => c.Spec.Validation.ValidateOnUp, context.ParseResult.GetValueForOption(CLIOptions.Validation.ValidateOnUpOption));
    config.UpdateConfig(c => c.Spec.Validation.ValidateOnUpdate, context.ParseResult.GetValueForOption(CLIOptions.Validation.ValidateOnUpdateOption));
    config.UpdateConfig(c => c.Spec.Validation.ReconcileOnUp, context.ParseResult.GetValueForOption(CLIOptions.Validation.ReconcileOnUpOption));
    config.UpdateConfig(c => c.Spec.Validation.ReconcileOnUpdate, context.ParseResult.GetValueForOption(CLIOptions.Validation.ReconcileOnUpdateOption));
    config.UpdateConfig(c => c.Spec.Validation.Verbose, context.ParseResult.GetValueForOption(CLIOptions.Validation.VerboseOption));

    Console.WriteLine($"✔ '{configFilePath}' configuration loaded.");
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
