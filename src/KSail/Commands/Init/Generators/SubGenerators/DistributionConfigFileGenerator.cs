using System.Text;
using Devantler.KubernetesGenerator.K3d;
using Devantler.KubernetesGenerator.K3d.Models;
using Devantler.KubernetesGenerator.K3d.Models.Options;
using Devantler.KubernetesGenerator.K3d.Models.Options.K3s;
using Devantler.KubernetesGenerator.K3d.Models.Registries;
using Devantler.KubernetesGenerator.Kind;
using Devantler.KubernetesGenerator.Kind.Models;
using Devantler.KubernetesGenerator.Kind.Models.Networking;
using k8s.Models;
using KSail.Models;
using KSail.Models.Project.Enums;

namespace KSail.Commands.Init.Generators.SubGenerators;

class DistributionConfigFileGenerator
{
  readonly K3dConfigGenerator _k3dConfigKubernetesGenerator = new();
  readonly KindConfigGenerator _kindConfigKubernetesGenerator = new();

  internal async Task GenerateAsync(KSailCluster config, CancellationToken cancellationToken = default)
  {
    string configPath = Path.Combine(config.Spec.Project.DistributionConfigPath);
    if (File.Exists(configPath))
    {
      Console.WriteLine($"✔ skipping '{configPath}', as it already exists.");
      return;
    }
    switch (config.Spec.Project.Engine, config.Spec.Project.Distribution)
    {
      case (KSailEngineType.Docker, KSailKubernetesDistributionType.Native):
        await GenerateKindConfigFile(config, configPath, cancellationToken).ConfigureAwait(false);
        break;
      case (KSailEngineType.Docker, KSailKubernetesDistributionType.K3s):
        await GenerateK3DConfigFile(config, configPath, cancellationToken).ConfigureAwait(false);
        break;
      default:
        throw new NotSupportedException($"Distribution '{config.Spec.Project.Distribution}' is not supported.");
    }
  }

  async Task GenerateKindConfigFile(KSailCluster config, string outputPath, CancellationToken cancellationToken = default)
  {
    Console.WriteLine($"✚ generating '{outputPath}'");
    var kindConfig = new KindConfig
    {
      Name = config.Metadata.Name,
      ContainerdConfigPatches = config.Spec.Project.MirrorRegistries ? [
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

    await _kindConfigKubernetesGenerator.GenerateAsync(kindConfig, outputPath, cancellationToken: cancellationToken).ConfigureAwait(false);
  }

  async Task GenerateK3DConfigFile(KSailCluster config, string outputPath, CancellationToken cancellationToken = default)
  {
    Console.WriteLine($"✚ generating '{outputPath}'");
    var mirrors = new StringBuilder();
    mirrors = mirrors.AppendLine("mirrors:");
    foreach (var registry in config.Spec.MirrorRegistries)
    {
      string mirror = $"""
      "{registry.Name}":
        endpoint:
          - http://host.k3d.internal:{registry.HostPort}
      """;
      mirror = string.Join(Environment.NewLine, mirror.Split(Environment.NewLine).Select(line => "    " + line));
      mirrors = mirrors.AppendLine(mirror);
    }
    var k3dConfig = new K3dConfig
    {
      Metadata = new V1ObjectMeta
      {
        Name = config.Metadata.Name
      },
      Registries = new K3dRegistries
      {
        Config = $"""
          {mirrors}
        """
      }
    };

    if (config.Spec.Project.CNI != KSailCNIType.Default)
    {
      k3dConfig.Options = new K3dOptions
      {
        K3s = new K3dOptionsK3s
        {
          ExtraArgs =
          [
            new K3dOptionsK3sExtraArg
            {
              Arg = "--flannel-backend=none"
            }
          ]
        }
      };
    }

    await _k3dConfigKubernetesGenerator.GenerateAsync(k3dConfig, outputPath, cancellationToken: cancellationToken).ConfigureAwait(false);
  }
}
