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
    CheckCompatibility(config);
    CheckContextName(projectRootPath, config.Spec.Project.Distribution, config.Metadata.Name, config.Spec.Connection.Context);
    CheckOCISourceUri(projectRootPath, config.Spec.Project.Distribution);

    var deserializer = new DeserializerBuilder().WithNamingConvention(CamelCaseNamingConvention.Instance).Build();
    switch (config.Spec.Project.Distribution)
    {
      case KSailDistributionType.K3d:
        {
          var distributionConfig = deserializer.Deserialize<K3dConfig>(await File.ReadAllTextAsync(Path.Combine(projectRootPath, config.Spec.Project.DistributionConfigPath), cancellationToken).ConfigureAwait(false));
          CheckClusterName(projectRootPath, config.Metadata.Name, distributionConfig.Metadata.Name);
          CheckK3dCNI(projectRootPath, distributionConfig);
          CheckK3dCSI(projectRootPath, distributionConfig);
          CheckK3dIngressController(projectRootPath, distributionConfig);
          CheckK3dMirrorRegistries(projectRootPath, distributionConfig);
          break;
        }

      case KSailDistributionType.Kind:
        {
          var distributionConfig = deserializer.Deserialize<KindConfig>(await File.ReadAllTextAsync(Path.Combine(projectRootPath, config.Spec.Project.DistributionConfigPath), cancellationToken).ConfigureAwait(false));
          CheckClusterName(projectRootPath, config.Metadata.Name, distributionConfig.Name);
          CheckKindCNI(projectRootPath, distributionConfig);
          break;
        }

      default:
        throw new NotSupportedException($"unsupported distribution '{config.Spec.Project.Distribution}'.");
    }
    Console.WriteLine("✔ configuration is valid");
  }

  static void CheckCompatibility(KSailCluster config)
  {
    // TODO: Remove temporary MacOS + Podman + K3d compatability check when the issue is resolved.
    if (OperatingSystem.IsMacOS() && config.Spec.Project.ContainerEngine == KSailContainerEngineType.Podman && config.Spec.Project.Distribution == KSailDistributionType.K3d)
    {
      throw new KSailException("Podman + K3d is not supported on MacOS yet." + Environment.NewLine
        + "  - 'host-gateway' is not working with 'podman machine' VMs." + Environment.NewLine
        + "    see https://github.com/containers/podman/issues/21681 for more details.");
    }
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
      KSailDistributionType.K3d => $"k3d-{name}",
      KSailDistributionType.Kind => $"kind-{name}",
      _ => throw new NotSupportedException($"unsupported distribution '{distribution}'.")
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
      KSailDistributionType.Kind => new Uri("oci://ksail-registry:5000/ksail-registry"),
      KSailDistributionType.K3d => new Uri("oci://host.k3d.internal:5555/ksail-registry"),
      _ => throw new NotSupportedException($"unsupported distribution '{distribution}'.")
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
    if (config.Spec.Project.CNI is KSailCNIType.Default && actual.Intersect(expectedWithCustomCNI).Any())
    {
      throw new KSailException($"'spec.project.cni={config.Spec.Project.CNI}' in '{Path.Combine(projectRootPath, config.Spec.Project.ConfigPath)}' does not match expected values in '{Path.Combine(projectRootPath, config.Spec.Project.DistributionConfigPath)}'." + Environment.NewLine +
        $"  - please remove '--flannel-backend=none' and '--disable-network-policy' from 'options.k3s.extraArgs' in '{Path.Combine(projectRootPath, config.Spec.Project.DistributionConfigPath)}'.");
    }
    else if (config.Spec.Project.CNI is not KSailCNIType.Default && (!actual.Any() || !actual.All(expectedWithCustomCNI.Contains)))
    {
      throw new KSailException($"'spec.project.cni={config.Spec.Project.CNI}' in '{Path.Combine(projectRootPath, config.Spec.Project.ConfigPath)}' does not match expected values in '{Path.Combine(projectRootPath, config.Spec.Project.DistributionConfigPath)}'." + Environment.NewLine +
        $"  - please set 'options.k3s.extraArgs' to '--flannel-backend=none' and '--disable-network-policy' for 'server:*' in '{Path.Combine(projectRootPath, config.Spec.Project.DistributionConfigPath)}'.");
    }
  }

  void CheckK3dCSI(string projectRootPath, K3dConfig distributionConfig)
  {
    var expectedWithCustomCSIK3sExtraArgs = new List<K3dOptionsK3sExtraArg>
      {
        new() {
          Arg = "--disable=local-storage",
          NodeFilters =
          [
            "server:*"
          ]
        }
      };
    var expectedWithCustomCSI = expectedWithCustomCSIK3sExtraArgs.Select(x => x.Arg + ":" + x.NodeFilters?.First()) ?? [];
    var actual = distributionConfig.Options?.K3s?.ExtraArgs?.Select(x => x.Arg + ":" + (x.NodeFilters?.First() ?? "server:*")) ?? [];
    if (config.Spec.Project.CSI is KSailCSIType.Default && actual.Intersect(expectedWithCustomCSI).Any())
    {
      throw new KSailException($"'spec.project.csi={config.Spec.Project.CSI}' in '{Path.Combine(projectRootPath, config.Spec.Project.ConfigPath)}' does not match expected values in '{Path.Combine(projectRootPath, config.Spec.Project.DistributionConfigPath)}'." + Environment.NewLine +
        $"  - please remove '--disable=local-storage' from 'options.k3s.extraArgs' in '{Path.Combine(projectRootPath, config.Spec.Project.DistributionConfigPath)}'.");
    }
    else if (config.Spec.Project.CSI is not KSailCSIType.Default && (!actual.Any() || !actual.All(expectedWithCustomCSI.Contains)))
    {
      throw new KSailException($"'spec.project.csi={config.Spec.Project.CSI}' in '{Path.Combine(projectRootPath, config.Spec.Project.ConfigPath)}' does not match expected values in '{Path.Combine(projectRootPath, config.Spec.Project.DistributionConfigPath)}'." + Environment.NewLine +
        $"  - please set 'options.k3s.extraArgs' to '--disable=local-storage' for 'server:*' in '{Path.Combine(projectRootPath, config.Spec.Project.DistributionConfigPath)}'.");
    }
  }

  void CheckK3dIngressController(string projectRootPath, K3dConfig distributionConfig)
  {
    var expectedWithCustomIngressControllerK3sExtraArgs = new List<K3dOptionsK3sExtraArg>
    {
      new() {
        Arg = "--disable=traefik",
        NodeFilters =
        [
          "server:*"
        ]
      }
    };
    var expectedWithCustomIngressController = expectedWithCustomIngressControllerK3sExtraArgs.Select(x => x.Arg + ":" + x.NodeFilters?.First()) ?? [];
    var actual = distributionConfig.Options?.K3s?.ExtraArgs?.Select(x => x.Arg + ":" + (x.NodeFilters?.First() ?? "server:*")) ?? [];
    if (config.Spec.Project.IngressController is KSailIngressControllerType.Default or KSailIngressControllerType.Traefik && actual.Intersect(expectedWithCustomIngressController).Any())
    {
      throw new KSailException($"'spec.project.ingressController={config.Spec.Project.IngressController}' in '{Path.Combine(projectRootPath, config.Spec.Project.ConfigPath)}' does not match expected values in '{Path.Combine(projectRootPath, config.Spec.Project.DistributionConfigPath)}'." + Environment.NewLine +
        $"  - please remove '--disable=traefik' from 'options.k3s.extraArgs' in '{Path.Combine(projectRootPath, config.Spec.Project.DistributionConfigPath)}'.");
    }
    else if (config.Spec.Project.IngressController is not KSailIngressControllerType.Default and not KSailIngressControllerType.Traefik && (!actual.Any() || !actual.All(expectedWithCustomIngressController.Contains)))
    {
      throw new KSailException($"'spec.project.ingressController={config.Spec.Project.IngressController}' in '{Path.Combine(projectRootPath, config.Spec.Project.ConfigPath)}' does not match expected values in '{Path.Combine(projectRootPath, config.Spec.Project.DistributionConfigPath)}'." + Environment.NewLine +
        $"  - please set 'options.k3s.extraArgs' to '--disable=traefik' for 'server:*' in '{Path.Combine(projectRootPath, config.Spec.Project.DistributionConfigPath)}'.");
    }
  }

  void CheckKindCNI(string projectRootPath, KindConfig distributionConfig)
  {
    if (config.Spec.Project.CNI is KSailCNIType.Default && distributionConfig.Networking?.DisableDefaultCNI == true)
    {
      throw new KSailException($"'spec.project.cni={config.Spec.Project.CNI}' in '{Path.Combine(projectRootPath, config.Spec.Project.ConfigPath)}' does not match expected values in '{Path.Combine(projectRootPath, config.Spec.Project.DistributionConfigPath)}'." + Environment.NewLine +
        $"  - please set 'networking.disableDefaultCNI: false' in '{Path.Combine(projectRootPath, config.Spec.Project.DistributionConfigPath)}'.");
    }
    else if (config.Spec.Project.CNI is not KSailCNIType.Default && distributionConfig.Networking?.DisableDefaultCNI != true)
    {
      throw new KSailException($"'spec.project.cni={config.Spec.Project.CNI}' in '{Path.Combine(projectRootPath, config.Spec.Project.ConfigPath)}' does not match expected values in '{Path.Combine(projectRootPath, config.Spec.Project.DistributionConfigPath)}'." + Environment.NewLine +
        $"  - please set 'networking.disableDefaultCNI: true' in '{Path.Combine(projectRootPath, config.Spec.Project.DistributionConfigPath)}'.");
    }
  }
}
