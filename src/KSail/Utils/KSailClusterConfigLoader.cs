using System.CommandLine.Invocation;
using Devantler.KubernetesGenerator.Core.Converters;
using Devantler.KubernetesGenerator.Core.Inspectors;
using KSail.Models;
using KSail.Models.Project;
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

  internal static async Task<KSailCluster> LoadWithGlobalOptionsAsync(GlobalOptions globalOptions, InvocationContext context)
  {
    var metadataNameOption = (MetadataNameOption)globalOptions.Options.First(o => o is MetadataNameOption);
    var projectConfigOption = (PathOption)globalOptions.Options.First(o => o is PathOption && o.Aliases.Contains("--config"));
    var projectDistributionOption = (ProjectDistributionOption)globalOptions.Options.First(o => o is ProjectDistributionOption);
    var projectWorkingDirectoryOption = (PathOption)globalOptions.Options.First(o => o is PathOption && o.Aliases.Contains("--working-directory"));
    var config = await LoadAsync(
      context.ParseResult.GetValueForOption(projectConfigOption),
      context.ParseResult.GetValueForOption(projectWorkingDirectoryOption),
      context.ParseResult.GetValueForOption(metadataNameOption),
      context.ParseResult.GetValueForOption(projectDistributionOption)
    ).ConfigureAwait(false);
    config.UpdateConfig("Metadata.Name", context.ParseResult.GetValueForOption(metadataNameOption));
    config.UpdateConfig("Spec.Connection.Kubeconfig", context.ParseResult.GetValueForOption((ConnectionKubeconfigOption)globalOptions.Options.First(o => o is ConnectionKubeconfigOption)));
    config.UpdateConfig("Spec.Connection.Context", context.ParseResult.GetValueForOption((ConnectionContextOption)globalOptions.Options.First(o => o is ConnectionContextOption)));
    config.UpdateConfig("Spec.Connection.Timeout", context.ParseResult.GetValueForOption((ConnectionTimeoutOption)globalOptions.Options.First(o => o is ConnectionTimeoutOption)));
    config.UpdateConfig("Spec.Project.WorkingDirectory", context.ParseResult.GetValueForOption(projectWorkingDirectoryOption));
    config.UpdateConfig("Spec.Project.ConfigPath", context.ParseResult.GetValueForOption(projectConfigOption));
    config.UpdateConfig("Spec.Project.Distribution", context.ParseResult.GetValueForOption(projectDistributionOption));
    config.UpdateConfig("Spec.Project.DistributionConfigPath", context.ParseResult.GetValueForOption((PathOption)globalOptions.Options.First(o => o is PathOption && o.Aliases.Contains("--distribution-config"))));
    config.UpdateConfig("Spec.Project.Engine", context.ParseResult.GetValueForOption((ProjectEngineOption)globalOptions.Options.First(o => o is ProjectEngineOption)));
    config.UpdateConfig("Spec.Project.MirrorRegistries", context.ParseResult.GetValueForOption((ProjectMirrorRegistriesOption)globalOptions.Options.First(o => o is ProjectMirrorRegistriesOption)));
    config.UpdateConfig("Spec.Project.SecretManager", context.ParseResult.GetValueForOption((ProjectSecretManagerOption)globalOptions.Options.First(o => o is ProjectSecretManagerOption)));
    config.UpdateConfig("Spec.Project.Template", context.ParseResult.GetValueForOption((ProjectTemplateOption)globalOptions.Options.First(o => o is ProjectTemplateOption)));
    config.UpdateConfig("Spec.Project.Editor", context.ParseResult.GetValueForOption((ProjectEditorOption)globalOptions.Options.First(o => o is ProjectEditorOption)));
    return config;
  }

  internal static async Task<KSailCluster> LoadAsync(string? configFilePath = "ksail-config.yaml", string? directory = default, string? name = default, KSailKubernetesDistribution distribution = default)
  {
    // Create default KSailClusterConfig
    var ksailClusterConfig = string.IsNullOrEmpty(name) ?
      new KSailCluster(distribution: distribution) :
      new KSailCluster(name, distribution: distribution);

    // Locate KSail YAML file
    string startDirectory = directory ?? Directory.GetCurrentDirectory();
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
