using System.ComponentModel;
using System.Runtime.InteropServices;
using KSail.Models.CLI;
using KSail.Models.CNI;
using KSail.Models.Connection;
using KSail.Models.DeploymentTool;
using KSail.Models.MirrorRegistry;
using KSail.Models.Project;
using KSail.Models.Registry;
using KSail.Models.SecretManager;
using KSail.Models.Template;
using YamlDotNet.Serialization;

namespace KSail.Models;

/// <summary>
/// The KSail cluster specification.
/// </summary>
public class KSailClusterSpec
{
  /// <summary>
  /// The options for connecting to the KSail cluster.
  /// </summary>
  [Description("The options for connecting to the KSail cluster.")]
  public KSailConnection Connection { get; set; } = new();

  /// <summary>
  /// The options for the KSail project.
  /// </summary>
  [Description("The options for the KSail project.")]
  public KSailProject Project { get; set; } = new();

  /// <summary>
  /// The options for the Flux deployment tool.
  /// </summary>
  [Description("The options for the Flux deployment tool.")]
  public KSailFluxDeploymentTool FluxDeploymentTool { get; set; } = new();

  /// <summary>
  /// The options for the Kustomize template.
  /// </summary>
  [Description("The options for the Kustomize template.")]
  public KSailKustomizeTemplate KustomizeTemplate { get; set; } = new();

  /// <summary>
  /// The options for the SOPS Secret Manager.
  /// </summary>
  [Description("The options for the SOPS Secret Manager.")]
  [YamlMember(Alias = "sopsSecretManager")]
  public KSailSOPSSecretManager SOPSSecretManager { get; set; } = new();

  /// <summary>
  /// The options for the Cilium CNI.
  /// </summary>
  [Description("The options for the Cilium CNI.")]
  public KSailCiliumCNI CiliumCNI { get; set; } = new();

  /// <summary>
  /// The ksail registry for storing deployment artifacts.
  /// </summary>
  [Description("The ksail registry for storing deployment artifacts.")]
  public KSailRegistry KSailRegistry { get; set; } = new KSailRegistry
  {
    Name = "ksail-registry",
    HostPort = 5555
  };

  /// <summary>
  /// The mirror registries to create for the KSail cluster.
  /// </summary>
  [Description("The mirror registries to create for the KSail cluster.")]
  public IEnumerable<KSailMirrorRegistry> MirrorRegistries { get; set; } = [
    new KSailMirrorRegistry { Name = "registry.k8s.io", HostPort = 5556, Proxy = new KSailMirrorRegistryProxy { Url = new Uri("https://registry.k8s.io") } },
    new KSailMirrorRegistry { Name = "docker.io", HostPort = 5557,  Proxy = new KSailMirrorRegistryProxy { Url = new Uri("https://registry-1.docker.io") } },
    new KSailMirrorRegistry { Name = "ghcr.io", HostPort = 5558, Proxy = new KSailMirrorRegistryProxy { Url = new Uri("https://ghcr.io") } },
    new KSailMirrorRegistry { Name = "gcr.io", HostPort = 5559, Proxy = new KSailMirrorRegistryProxy { Url = new Uri("https://gcr.io") } },
    new KSailMirrorRegistry { Name = "mcr.microsoft.com", HostPort = 5560, Proxy = new KSailMirrorRegistryProxy { Url = new Uri("https://mcr.microsoft.com") } },
    new KSailMirrorRegistry { Name = "quay.io", HostPort = 5561, Proxy = new KSailMirrorRegistryProxy { Url = new Uri("https://quay.io") } },
  ];

  /// <summary>
  /// The CLI options.
  /// </summary>
  [Description("The CLI options.")]
  [YamlMember(Alias = "cli")]
  public KSailCLI CLI { get; set; } = new();

  /// <summary>
  /// Initializes a new instance of the <see cref="KSailClusterSpec"/> class.
  /// </summary>
  public KSailClusterSpec() => SetOCISourceUriBasedOnOS();

  /// <summary>
  /// Initializes a new instance of the <see cref="KSailClusterSpec"/> class.
  /// </summary>
  /// <param name="name"></param>
  public KSailClusterSpec(string name)
  {
    SetOCISourceUriBasedOnOS();
    Connection = new KSailConnection
    {
      Context = $"kind-{name}"
    };
    KustomizeTemplate = new KSailKustomizeTemplate
    {
      Root = $"k8s/clusters/{name}/flux-system"
    };
  }

  /// <summary>
  /// Initializes a new instance of the <see cref="KSailClusterSpec"/> class with the specified distribution.
  /// </summary>
  /// <param name="name"></param>
  /// <param name="distribution"></param>
  public KSailClusterSpec(string name, KSailKubernetesDistribution distribution) : this(name)
  {
    SetOCISourceUriBasedOnOS();
    Connection = new KSailConnection
    {
      Context = distribution switch
      {
        KSailKubernetesDistribution.Native => $"kind-{name}",
        KSailKubernetesDistribution.K3s => $"k3d-{name}",
        _ => $"kind-{name}"
      }
    };
    Project = new KSailProject
    {
      Distribution = distribution,
      DistributionConfigPath = distribution switch
      {
        KSailKubernetesDistribution.Native => "kind-config.yaml",
        KSailKubernetesDistribution.K3s => "k3d-config.yaml",
        _ => "kind-config.yaml"
      }
    };
    KustomizeTemplate = new KSailKustomizeTemplate
    {
      Root = $"k8s/clusters/{name}/flux-system"
    };
  }

  void SetOCISourceUriBasedOnOS()
  {
    if (RuntimeInformation.IsOSPlatform(OSPlatform.Linux))
    {
      FluxDeploymentTool = new KSailFluxDeploymentTool(new Uri("oci://172.17.0.1:5555/ksail-registry"));
    }
    else if (RuntimeInformation.IsOSPlatform(OSPlatform.OSX))
    {
      FluxDeploymentTool = new KSailFluxDeploymentTool(new Uri("oci://host.docker.internal:5555/ksail-registry"));
    }
  }
}
