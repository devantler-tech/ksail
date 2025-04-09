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
  public async Task<(bool, string)> ValidateAsync(string path, CancellationToken cancellationToken = default)
  {
    string projectRootPath = path;
    try
    {
      while (!File.Exists(Path.Combine(projectRootPath, config.Spec.Project.ConfigPath)))
      {
        projectRootPath = Directory.GetParent(projectRootPath)?.FullName ?? throw new KSailException($"not a valid ksail project directory or subdirectory: '{path}'.");
      }
    }
    catch (Exception)
    {
      return (true, string.Empty);
    }

    var (isValid, message) = CheckContextName(projectRootPath, config.Spec.Project.Distribution, config.Metadata.Name, config.Spec.Connection.Context);
    if (!isValid)
      return (false, message);


    (isValid, message) = CheckOCISourceUri(projectRootPath, config.Spec.Project.Distribution);
    if (!isValid)
      return (false, message);

    var deserializer = new DeserializerBuilder().WithNamingConvention(CamelCaseNamingConvention.Instance).Build();
    if (config.Spec.Project.Distribution == KSailDistributionType.K3s)
    {
      var distributionConfig = deserializer.Deserialize<K3dConfig>(await File.ReadAllTextAsync(Path.Combine(projectRootPath, config.Spec.Project.DistributionConfigPath), cancellationToken).ConfigureAwait(false));

      (isValid, message) = CheckClusterName(projectRootPath, config.Metadata.Name, distributionConfig.Metadata.Name);
      if (!isValid)
        return (false, message);

      (isValid, message) = CheckK3dCNI(projectRootPath, distributionConfig);
      if (!isValid)
        return (false, message);

      (isValid, message) = CheckK3dMirrorRegistries(projectRootPath, distributionConfig);
      if (!isValid)
        return (false, message);
    }
    else if (config.Spec.Project.Distribution == KSailDistributionType.Native)
    {
      var distributionConfig = deserializer.Deserialize<KindConfig>(await File.ReadAllTextAsync(Path.Combine(projectRootPath, config.Spec.Project.DistributionConfigPath), cancellationToken).ConfigureAwait(false));
      (isValid, message) = CheckClusterName(projectRootPath, config.Metadata.Name, distributionConfig.Name);
      if (!isValid)
        return (false, message);

      (isValid, message) = CheckKindCNI(projectRootPath, distributionConfig);
      if (!isValid)
        return (false, message);
    }
    else
    {
      throw new KSailException($"unsupported distribution '{config.Spec.Project.Distribution}'.");
    }
    return (true, string.Empty);
  }

  (bool isValid, string message) CheckK3dMirrorRegistries(string projectRootPath, K3dConfig distributionConfig)
  {
    // check that k3d config includes all mirrors from the ksail config
    var expectedMirrors = config.Spec.MirrorRegistries.Select(x => x.Proxy.Url.Host.Contains("docker.io", StringComparison.Ordinal) ? "docker.io" : x.Proxy.Url.Host);
    foreach (string? expectedMirror in expectedMirrors)
    {
      if (!(distributionConfig.Registries?.Config?.Contains(expectedMirror, StringComparison.Ordinal) ?? false))
      {
        return (false, $"'registries.config' in '{Path.Combine(projectRootPath, config.Spec.Project.DistributionConfigPath)}' does not contain the expected mirror '{expectedMirror}'." + Environment.NewLine +
          $"  - please add the mirror to 'registries.config' in '{Path.Combine(projectRootPath, config.Spec.Project.DistributionConfigPath)}'.");
      }
    }
    return (true, string.Empty);
  }

  (bool isValid, string message) CheckContextName(string projectRootPath, KSailDistributionType distribution, string name, string context)
  {
    string expectedContextName = distribution switch
    {
      KSailDistributionType.K3s => $"k3d-{name}",
      KSailDistributionType.Native => $"kind-{name}",
      _ => throw new KSailException($"unsupported distribution '{distribution}'.")
    };
    return !string.Equals(expectedContextName, context, StringComparison.Ordinal)
      ? (false, $"'config.spec.connection.context' in '{Path.Combine(projectRootPath, config.Spec.Project.ConfigPath)}' does not match the expected value '{expectedContextName}'.")
      : (true, string.Empty);
  }

  (bool isValid, string message) CheckOCISourceUri(string projectRootPath, KSailDistributionType distribution)
  {
    var expectedOCISourceUri = distribution switch
    {
      KSailDistributionType.Native => new Uri("oci://ksail-registry:5000/ksail-registry"),
      KSailDistributionType.K3s => new Uri("oci://host.k3d.internal:5555/ksail-registry"),
      _ => throw new KSailException($"unsupported distribution '{distribution}'.")
    };
    return !Equals(expectedOCISourceUri, config.Spec.DeploymentTool.Flux.Source.Url)
      ? (false, $"'config.spec.deploymentTool.flux.source.url' in '{Path.Combine(projectRootPath, config.Spec.Project.ConfigPath)}' does not match the expected value '{expectedOCISourceUri}'.")
      : (true, string.Empty);
  }

  (bool, string) CheckClusterName(string projectRootPath, string ksailClusterName, string distributionClusterName)
  {
    return !string.Equals(ksailClusterName, distributionClusterName, StringComparison.Ordinal)
      ? (false, $"'metadata.name' in '{Path.Combine(projectRootPath, config.Spec.Project.ConfigPath)}' does not match cluster name in '{Path.Combine(projectRootPath, config.Spec.Project.DistributionConfigPath)}'." + Environment.NewLine +
        $"  - please set cluster name to '{ksailClusterName}' in '{Path.Combine(projectRootPath, config.Spec.Project.DistributionConfigPath)}'.")
      : (true, string.Empty);
  }

  (bool, string) CheckK3dCNI(string projectRootPath, K3dConfig distributionConfig)
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
      return (false, $"'spec.project.cni={config.Spec.Project.CNI}' in '{Path.Combine(projectRootPath, config.Spec.Project.ConfigPath)}' does not match expected values in '{Path.Combine(projectRootPath, config.Spec.Project.DistributionConfigPath)}'." + Environment.NewLine +
        $"  - please remove '--flannel-backend=none' and '--disable-network-policy' from 'options.k3s.extraArgs' in '{Path.Combine(projectRootPath, config.Spec.Project.DistributionConfigPath)}'.");
    }
    else if (config.Spec.Project.CNI != KSailCNIType.Default && !actual.All(expectedWithCustomCNI.Contains))
    {
      return (false, $"'spec.project.cni={config.Spec.Project.CNI}' in '{Path.Combine(projectRootPath, config.Spec.Project.ConfigPath)}' does not match expected values in '{Path.Combine(projectRootPath, config.Spec.Project.DistributionConfigPath)}'." + Environment.NewLine +
        $"  - please set 'options.k3s.extraArgs' to '--flannel-backend=none' and '--disable-network-policy' for 'server:*' in '{Path.Combine(projectRootPath, config.Spec.Project.DistributionConfigPath)}'.");
    }
    return (true, string.Empty);
  }

  (bool isValid, string message) CheckKindCNI(string projectRootPath, KindConfig distributionConfig)
  {
    if (config.Spec.Project.CNI == KSailCNIType.Default && distributionConfig.Networking?.DisableDefaultCNI == true)
    {
      return (false, $"'spec.project.cni={config.Spec.Project.CNI}' in '{Path.Combine(projectRootPath, config.Spec.Project.ConfigPath)}' does not match expected values in '{Path.Combine(projectRootPath, config.Spec.Project.DistributionConfigPath)}'." + Environment.NewLine +
        $"  - please set 'networking.disableDefaultCNI: false' in '{Path.Combine(projectRootPath, config.Spec.Project.DistributionConfigPath)}'.");
    }
    else if (config.Spec.Project.CNI != KSailCNIType.Default && distributionConfig.Networking?.DisableDefaultCNI != true)
    {
      return (false, $"'spec.project.cni={config.Spec.Project.CNI}' in '{Path.Combine(projectRootPath, config.Spec.Project.ConfigPath)}' does not match expected values in '{Path.Combine(projectRootPath, config.Spec.Project.DistributionConfigPath)}'." + Environment.NewLine +
        $"  - please set 'networking.disableDefaultCNI: true' in '{Path.Combine(projectRootPath, config.Spec.Project.DistributionConfigPath)}'.");
    }
    return (true, string.Empty);
  }
}
