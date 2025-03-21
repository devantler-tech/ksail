using Devantler.KubernetesGenerator.K3d.Models;
using Devantler.KubernetesGenerator.K3d.Models.Options.K3s;
using Devantler.KubernetesGenerator.Kind.Models;
using KSail.Models;
using KSail.Models.Project.Enums;
using YamlDotNet.Serialization;
using YamlDotNet.Serialization.NamingConventions;

namespace KSail.Commands.Lint;

internal class ConfigurationValidator(KSailCluster config)
{
  public async Task<(bool, string)> ValidateAsync(CancellationToken cancellationToken = default)
  {
    var (isValid, message) = CheckContextName(config.Spec.Project.Distribution, config.Metadata.Name, config.Spec.Connection.Context);
    if (!isValid)
      return (false, message);


    (isValid, message) = CheckOCISourceUri(config.Spec.Project.Distribution);
    if (!isValid)
      return (false, message);

    var deserializer = new DeserializerBuilder().WithNamingConvention(CamelCaseNamingConvention.Instance).Build();
    if (config.Spec.Project.Distribution == KSailKubernetesDistributionType.K3s)
    {
      var distributionConfig = deserializer.Deserialize<K3dConfig>(await File.ReadAllTextAsync(config.Spec.Project.DistributionConfigPath, cancellationToken).ConfigureAwait(false));

      (isValid, message) = CheckClusterName(config.Metadata.Name, distributionConfig.Metadata.Name);
      if (!isValid)
        return (false, message);

      (isValid, message) = CheckK3dCNI(distributionConfig);
      if (!isValid)
        return (false, message);

      (isValid, message) = CheckK3dMirrorRegistries(distributionConfig);
      if (!isValid)
        return (false, message);
    }
    else if (config.Spec.Project.Distribution == KSailKubernetesDistributionType.Native)
    {
      var distributionConfig = deserializer.Deserialize<KindConfig>(await File.ReadAllTextAsync(config.Spec.Project.DistributionConfigPath, cancellationToken).ConfigureAwait(false));
      (isValid, message) = CheckClusterName(config.Metadata.Name, distributionConfig.Name);
      if (!isValid)
        return (false, message);

      (isValid, message) = CheckKindCNI(distributionConfig);
      if (!isValid)
        return (false, message);
    }
    else
    {
      throw new KSailException($"unsupported distribution '{config.Spec.Project.Distribution}'.");
    }
    return (true, string.Empty);
  }

  private (bool isValid, string message) CheckK3dMirrorRegistries(K3dConfig distributionConfig)
  {
    // check that k3d config includes all mirrors from the ksail config
    var expectedMirrors = config.Spec.MirrorRegistries.Select(x => x.Proxy.Url.Host.Contains("docker.io", StringComparison.Ordinal) ? "docker.io" : x.Proxy.Url.Host);
    foreach (var expectedMirror in expectedMirrors)
    {
      if (!(distributionConfig.Registries?.Config?.Contains(expectedMirror, StringComparison.Ordinal) ?? false))
      {
        return (false, $"'registries.config' in '{config.Spec.Project.DistributionConfigPath}' does not contain the expected mirror '{expectedMirror}'." + Environment.NewLine +
          $"  - please add the mirror to 'registries.config' in '{config.Spec.Project.DistributionConfigPath}'.");
      }
    }
    return (true, string.Empty);
  }

  private (bool isValid, string message) CheckContextName(KSailKubernetesDistributionType distribution, string name, string context)
  {
    var expectedContextName = distribution switch
    {
      KSailKubernetesDistributionType.K3s => $"k3d-{name}",
      KSailKubernetesDistributionType.Native => $"kind-{name}",
      _ => throw new KSailException($"unsupported distribution '{distribution}'.")
    };
    if (!string.Equals(expectedContextName, context, StringComparison.Ordinal))
      return (false, $"'config.spec.connection.context' in '{config.Spec.Project.ConfigPath}' does not match the expected value '{expectedContextName}'.");
    else
    {
      return (true, string.Empty);
    }
  }

  private (bool isValid, string message) CheckOCISourceUri(KSailKubernetesDistributionType distribution)
  {
    var expectedOCISourceUri = distribution switch
    {
      KSailKubernetesDistributionType.Native => new Uri("oci://ksail-registry:5000/ksail-registry"),
      KSailKubernetesDistributionType.K3s => new Uri("oci://host.k3d.internal:5555/ksail-registry"),
      _ => throw new KSailException($"unsupported distribution '{distribution}'.")
    };
    if (!Equals(expectedOCISourceUri, config.Spec.DeploymentTool.Flux.Source.Url))
      return (false, $"'config.spec.deploymentTool.flux.source.url' in '{config.Spec.Project.ConfigPath}' does not match the expected value '{expectedOCISourceUri}'.");
    return (true, string.Empty);
  }

  private (bool, string) CheckClusterName(string ksailClusterName, string distributionClusterName)
  {
    if (!string.Equals(ksailClusterName, distributionClusterName, StringComparison.Ordinal))
    {
      return (false, $"'metadata.name' in '{config.Spec.Project.ConfigPath}' does not match cluster name in '{config.Spec.Project.DistributionConfigPath}'." + Environment.NewLine +
        $"  - please set cluster name to '{ksailClusterName}' in '{config.Spec.Project.DistributionConfigPath}'.");
    }
    else
    {
      return (true, string.Empty);
    }
  }

  private (bool, string) CheckK3dCNI(K3dConfig distributionConfig)
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
      return (false, $"'spec.project.cni={config.Spec.Project.CNI}' in '{config.Spec.Project.ConfigPath}' does not match expected values in '{config.Spec.Project.DistributionConfigPath}'." + Environment.NewLine +
        $"  - please remove '--flannel-backend=none' and '--disable-network-policy' from 'options.k3s.extraArgs' in '{config.Spec.Project.DistributionConfigPath}'.");
    }
    else if (config.Spec.Project.CNI != KSailCNIType.Default && !actual.All(expectedWithCustomCNI.Contains))
    {
      return (false, $"'spec.project.cni={config.Spec.Project.CNI}' in '{config.Spec.Project.ConfigPath}' does not match expected values in '{config.Spec.Project.DistributionConfigPath}'." + Environment.NewLine +
        $"  - please set 'options.k3s.extraArgs' to '--flannel-backend=none' and '--disable-network-policy' for 'server:*' in '{config.Spec.Project.DistributionConfigPath}'.");
    }
    return (true, string.Empty);
  }

  private (bool isValid, string message) CheckKindCNI(KindConfig distributionConfig)
  {
    if (config.Spec.Project.CNI == KSailCNIType.Default && distributionConfig.Networking?.DisableDefaultCNI == true)
    {
      return (false, $"'spec.project.cni={config.Spec.Project.CNI}' in '{config.Spec.Project.ConfigPath}' does not match expected values in '{config.Spec.Project.DistributionConfigPath}'." + Environment.NewLine +
        $"  - please set 'networking.disableDefaultCNI: false' in '{config.Spec.Project.DistributionConfigPath}'.");
    }
    else if (config.Spec.Project.CNI != KSailCNIType.Default && distributionConfig.Networking?.DisableDefaultCNI != true)
    {
      return (false, $"'spec.project.cni={config.Spec.Project.CNI}' in '{config.Spec.Project.ConfigPath}' does not match expected values in '{config.Spec.Project.DistributionConfigPath}'." + Environment.NewLine +
        $"  - please set 'networking.disableDefaultCNI: true' in '{config.Spec.Project.DistributionConfigPath}'.");
    }
    return (true, string.Empty);
  }
}
