using System.Text;
using Devantler.KubernetesGenerator.K3d;
using Devantler.KubernetesGenerator.K3d.Models;
using Devantler.KubernetesGenerator.Kind;
using Devantler.KubernetesGenerator.Kind.Models;
using Devantler.KubernetesGenerator.Kind.Models.Networking;
using KSail.Models;
using KSail.Models.Project.Enums;

namespace KSail.Commands.Init.Generators;

class DistributionConfigFileGenerator
{
  readonly K3dConfigGenerator _k3dConfigKubernetesGenerator = new();
  readonly KindConfigGenerator _kindConfigKubernetesGenerator = new();

  internal async Task GenerateAsync(string outputPath, KSailCluster config, CancellationToken cancellationToken = default)
  {
    string configPath = Path.Combine(outputPath, config.Spec.Project.DistributionConfigPath);
    switch (config.Spec.Project.Provider, config.Spec.Project.Distribution)
    {
      case (KSailProviderType.Docker, KSailDistributionType.Native):
        await GenerateKindConfigFile(config, configPath, cancellationToken).ConfigureAwait(false);
        break;
      case (KSailProviderType.Docker, KSailDistributionType.K3s):
        await GenerateK3DConfigFile(config, configPath, cancellationToken).ConfigureAwait(false);
        break;
      default:
        throw new NotSupportedException($"Distribution '{config.Spec.Project.Distribution}' is not supported.");
    }
  }

  async Task GenerateKindConfigFile(KSailCluster config, string outputPath, CancellationToken cancellationToken = default)
  {
    bool overwrite = config.Spec.Generator.Overwrite;
    Console.WriteLine(File.Exists(outputPath) ? overwrite ?
      $"✚ overwriting '{outputPath}'" :
      $"✔ skipping '{outputPath}', as it already exists." :
      $"✚ generating '{outputPath}'");
    if (File.Exists(outputPath) && !overwrite)
      return;
    var kindConfig = new KindConfig
    {
      Name = config.Metadata.Name,
      ContainerdConfigPatches = config.Spec.Project.MirrorRegistries ?
      [
        """
        [plugins."io.containerd.grpc.v1.cri".registry]
          config_path = "/etc/containerd/certs.d"
        """
      ] : null
    };

    if (config.Spec.Project.CNI != KSailCNIType.Default)
    {
      kindConfig.Networking = new KindNetworking
      {
        DisableDefaultCNI = true
      };
    }

    await _kindConfigKubernetesGenerator.GenerateAsync(kindConfig, outputPath, config.Spec.Generator.Overwrite, cancellationToken: cancellationToken).ConfigureAwait(false);
  }

  async Task GenerateK3DConfigFile(KSailCluster config, string outputPath, CancellationToken cancellationToken = default)
  {
    bool overwrite = config.Spec.Generator.Overwrite;
    Console.WriteLine(File.Exists(outputPath) ? overwrite ?
      $"✚ overwriting '{outputPath}'" :
      $"✔ skipping '{outputPath}', as it already exists." :
      $"✚ generating '{outputPath}'");
    if (File.Exists(outputPath) && !overwrite)
      return;
    var mirrors = new StringBuilder();
    mirrors = mirrors.AppendLine("mirrors:");
    foreach (var registry in config.Spec.MirrorRegistries)
    {
      string host = registry.Proxy.Url.Host.Contains("docker.io", StringComparison.OrdinalIgnoreCase) ? "docker.io" : registry.Proxy.Url.Host;
      string mirror = $"""
        "{host}":
          endpoint:
            - http://host.k3d.internal:{registry.HostPort}
        """;
      mirror = string.Join(Environment.NewLine, mirror.Split(Environment.NewLine).Select(line => "    " + line));
      mirrors = mirrors.AppendLine(mirror);
    }
    var k3dConfig = new K3dConfig
    {
      Metadata = new()
      {
        Name = config.Metadata.Name
      },
      Registries = new()
      {
        Config = $"""
          {mirrors}
        """
      }
    };

    if (config.Spec.Project.CNI != KSailCNIType.Default)
    {
      k3dConfig.Options = new()
      {
        K3s = new()
        {
          ExtraArgs =
          [
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
          ]
        }
      };
    }

    await _k3dConfigKubernetesGenerator.GenerateAsync(k3dConfig, outputPath, cancellationToken: cancellationToken).ConfigureAwait(false);
  }
}
