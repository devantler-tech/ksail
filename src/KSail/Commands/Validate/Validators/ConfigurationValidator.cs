using Devantler.KubernetesGenerator.K3d.Models;
using Devantler.KubernetesGenerator.K3d.Models.Options.K3s;
using Devantler.KubernetesGenerator.Kind.Models;
using KSail.Models;
using KSail.Models.Project.Enums;
using YamlDotNet.Serialization;
using YamlDotNet.Serialization.NamingConventions;

namespace KSail.Commands.Validate.Validators;

class ConfigurationValidator(KSailCluster config)
{
  public async Task ValidateAsync(string path, CancellationToken cancellationToken = default)
  {
    Console.WriteLine("► locating configuration files");
    string projectRootPath = path;
    try
    {
      projectRootPath = GetProjectRootPath(path, projectRootPath);
    }
    catch (Exception)
    {
      Console.WriteLine("✔ skipping configuration validation");
      Console.WriteLine("  - no configuration files found in the current directory or any parent directories.");
      return;
    }
    Console.WriteLine($"✔ configuration files located");
    Console.WriteLine("► validating configuration");
    CheckContextName(projectRootPath, config.Spec.Project.Distribution, config.Metadata.Name, config.Spec.Connection.Context);
    CheckOCISourceUri(projectRootPath, config.Spec.Project.Distribution);

    var deserializer = new DeserializerBuilder().WithNamingConvention(CamelCaseNamingConvention.Instance).Build();
    switch (config.Spec.Project.Distribution)
    {
      case KSailDistributionType.K3s:
        {
          var distributionConfig = deserializer.Deserialize<K3dConfig>(await File.ReadAllTextAsync(Path.Combine(projectRootPath, config.Spec.Project.DistributionConfigPath), cancellationToken).ConfigureAwait(false));
          CheckClusterName(projectRootPath, config.Metadata.Name, distributionConfig.Metadata.Name);
          CheckK3dCNI(projectRootPath, distributionConfig);
          CheckK3dMirrorRegistries(projectRootPath, distributionConfig);
          break;
        }

      case KSailDistributionType.Native:
        {
          var distributionConfig = deserializer.Deserialize<KindConfig>(await File.ReadAllTextAsync(Path.Combine(projectRootPath, config.Spec.Project.DistributionConfigPath), cancellationToken).ConfigureAwait(false));
          CheckClusterName(projectRootPath, config.Metadata.Name, distributionConfig.Name);
          CheckKindCNI(projectRootPath, distributionConfig);
          break;
        }

      default:
        throw new KSailException($"unsupported distribution '{config.Spec.Project.Distribution}'.");
    }
    Console.WriteLine("✔ configuration is valid");
  }

  string GetProjectRootPath(string path, string projectRootPath)
  {
    while (!File.Exists(Path.Combine(projectRootPath, config.Spec.Project.ConfigPath)))
    {
      projectRootPath = Directory.GetParent(projectRootPath)?.FullName ?? throw new KSailException($"not a valid ksail project directory or subdirectory: '{path}'.");
    }

    return projectRootPath;
  }

  void CheckK3dMirrorRegistries(string projectRootPath, K3dConfig distributionConfig)
  {
    var expectedMirrors = config.Spec.MirrorRegistries.Select(x => x.Proxy.Url.Host.Contains("docker.io", StringComparison.Ordinal) ? "docker.io" : x.Proxy.Url.Host) ?? [];
    foreach (string expectedMirror in expectedMirrors)
    {
      if (distributionConfig.Registries?.Config?.Contains(expectedMirror, StringComparison.Ordinal) != true)
      {
        throw new KSailException($"'registries.config' in '{Path.Combine(projectRootPath, config.Spec.Project.DistributionConfigPath)}' does not contain the expected mirror '{expectedMirror}'." + Environment.NewLine +
          $"  - please add the mirror to 'registries.config' in '{Path.Combine(projectRootPath, config.Spec.Project.DistributionConfigPath)}'.");
      }
    }
  }

  void CheckContextName(string projectRootPath, KSailDistributionType distribution, string name, string context)
  {
    string expectedContextName = distribution switch
    {
      KSailDistributionType.K3s => $"k3d-{name}",
      KSailDistributionType.Native => $"kind-{name}",
      _ => throw new KSailException($"unsupported distribution '{distribution}'.")
    };
    if (!string.Equals(expectedContextName, context, StringComparison.Ordinal))
    {
      throw new KSailException($"'config.spec.connection.context' in '{Path.Combine(projectRootPath, config.Spec.Project.ConfigPath)}' does not match the expected value '{expectedContextName}'.");
    }
  }

  void CheckOCISourceUri(string projectRootPath, KSailDistributionType distribution)
  {
    var expectedOCISourceUri = distribution switch
    {
      KSailDistributionType.Native => new Uri("oci://ksail-registry:5000/ksail-registry"),
      KSailDistributionType.K3s => new Uri("oci://host.k3d.internal:5555/ksail-registry"),
      _ => throw new KSailException($"unsupported distribution '{distribution}'.")
    };
    if (!Equals(expectedOCISourceUri, config.Spec.DeploymentTool.Flux.Source.Url))
    {
      throw new KSailException($"'config.spec.deploymentTool.flux.source.url' in '{Path.Combine(projectRootPath, config.Spec.Project.ConfigPath)}' does not match the expected value '{expectedOCISourceUri}'.");
    }
  }

  void CheckClusterName(string projectRootPath, string ksailClusterName, string distributionClusterName)
  {
    if (!string.Equals(ksailClusterName, distributionClusterName, StringComparison.Ordinal))
    {
      throw new KSailException($"'metadata.name' in '{Path.Combine(projectRootPath, config.Spec.Project.ConfigPath)}' does not match cluster name in '{Path.Combine(projectRootPath, config.Spec.Project.DistributionConfigPath)}'." + Environment.NewLine +
        $"  - please set cluster name to '{ksailClusterName}' in '{Path.Combine(projectRootPath, config.Spec.Project.DistributionConfigPath)}'.");
    }
  }

  void CheckK3dCNI(string projectRootPath, K3dConfig distributionConfig)
  {
    var expectedWithCustomCNIK3sExtraArgs = new List<K3dOptionsK3sExtraArg>
      {
        new() {
          Arg = "--flannel-backend=none",
          NodeFilters =
          [
            "server:*"
          ]
        },
        new() {
          Arg = "--disable-network-policy",
          NodeFilters =
          [
            "server:*"
          ]
        }
      };
    var expectedWithCustomCNI = expectedWithCustomCNIK3sExtraArgs.Select(x => x.Arg + ":" + x.NodeFilters?.First()) ?? [];
    var actual = distributionConfig.Options?.K3s?.ExtraArgs?.Select(x => x.Arg + ":" + (x.NodeFilters?.First() ?? "server:*")) ?? [];
    if (config.Spec.Project.CNI == KSailCNIType.Default && actual.Intersect(expectedWithCustomCNI).Any())
    {
      throw new KSailException($"'spec.project.cni={config.Spec.Project.CNI}' in '{Path.Combine(projectRootPath, config.Spec.Project.ConfigPath)}' does not match expected values in '{Path.Combine(projectRootPath, config.Spec.Project.DistributionConfigPath)}'." + Environment.NewLine +
        $"  - please remove '--flannel-backend=none' and '--disable-network-policy' from 'options.k3s.extraArgs' in '{Path.Combine(projectRootPath, config.Spec.Project.DistributionConfigPath)}'.");
    }
    else if (config.Spec.Project.CNI != KSailCNIType.Default && (!actual.Any() || !actual.All(expectedWithCustomCNI.Contains)))
    {
      throw new KSailException($"'spec.project.cni={config.Spec.Project.CNI}' in '{Path.Combine(projectRootPath, config.Spec.Project.ConfigPath)}' does not match expected values in '{Path.Combine(projectRootPath, config.Spec.Project.DistributionConfigPath)}'." + Environment.NewLine +
        $"  - please set 'options.k3s.extraArgs' to '--flannel-backend=none' and '--disable-network-policy' for 'server:*' in '{Path.Combine(projectRootPath, config.Spec.Project.DistributionConfigPath)}'.");
    }
  }

  void CheckKindCNI(string projectRootPath, KindConfig distributionConfig)
  {
    if (config.Spec.Project.CNI == KSailCNIType.Default && distributionConfig.Networking?.DisableDefaultCNI == true)
    {
      throw new KSailException($"'spec.project.cni={config.Spec.Project.CNI}' in '{Path.Combine(projectRootPath, config.Spec.Project.ConfigPath)}' does not match expected values in '{Path.Combine(projectRootPath, config.Spec.Project.DistributionConfigPath)}'." + Environment.NewLine +
        $"  - please set 'networking.disableDefaultCNI: false' in '{Path.Combine(projectRootPath, config.Spec.Project.DistributionConfigPath)}'.");
    }
    else if (config.Spec.Project.CNI != KSailCNIType.Default && distributionConfig.Networking?.DisableDefaultCNI != true)
    {
      throw new KSailException($"'spec.project.cni={config.Spec.Project.CNI}' in '{Path.Combine(projectRootPath, config.Spec.Project.ConfigPath)}' does not match expected values in '{Path.Combine(projectRootPath, config.Spec.Project.DistributionConfigPath)}'." + Environment.NewLine +
        $"  - please set 'networking.disableDefaultCNI: true' in '{Path.Combine(projectRootPath, config.Spec.Project.DistributionConfigPath)}'.");
    }
  }
}
