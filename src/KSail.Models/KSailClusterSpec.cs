using System.ComponentModel;
using KSail.Models.Connection;
using KSail.Models.DeploymentTool;
using KSail.Models.Distribution;
using KSail.Models.IngressController;
using KSail.Models.LocalRegistry;
using KSail.Models.MirrorRegistry;
using KSail.Models.Project;
using KSail.Models.Project.Enums;
using KSail.Models.SecretManager;
using KSail.Models.Validation;
using KSail.Models.WaypointController;
using YamlDotNet.Serialization;

namespace KSail.Models;


public class KSailClusterSpec
{

  [Description("The options for connecting to the KSail cluster.")]
  public KSailConnection Connection { get; set; } = new();

  [Description("The options for the KSail project.")]
  public KSailProject Project { get; set; } = new();

  [Description("The options for the deployment tool.")]
  public KSailDeploymentTool DeploymentTool { get; set; } = new();

  [Description("The options for the distribution.")]
  public KSailDistribution Distribution { get; set; } = new();

  [Description("The options for the Secret Manager.")]
  public KSailSecretManager SecretManager { get; set; } = new();

  [Description("The options for the CNI.")]
  [YamlMember(Alias = "cni")]
  public KSailCNIType CNI { get; set; } = new();

  [Description("The options for the Ingress Controller.")]
  public KSailIngressController IngressController { get; set; } = new();

  [Description("The options for the Waypoint Controller.")]
  public KSailWaypointController WaypointController { get; set; } = new();

  [Description("The local registry for storing deployment artifacts.")]
  public KSailLocalRegistry LocalRegistry { get; set; } = new KSailLocalRegistry
  {
    Name = "ksail-registry",
    HostPort = 5555
  };

  [Description("The mirror registries to create for the KSail cluster.")]
  public IEnumerable<KSailMirrorRegistry> MirrorRegistries { get; set; } = [
    new KSailMirrorRegistry { Name = "registry.k8s.io-proxy", HostPort = 5556, Proxy = new KSailMirrorRegistryProxy { Url = new Uri("https://registry.k8s.io") } },
    new KSailMirrorRegistry { Name = "docker.io-proxy", HostPort = 5557,  Proxy = new KSailMirrorRegistryProxy { Url = new Uri("https://registry-1.docker.io") } },
    new KSailMirrorRegistry { Name = "ghcr.io-proxy", HostPort = 5558, Proxy = new KSailMirrorRegistryProxy { Url = new Uri("https://ghcr.io") } },
    new KSailMirrorRegistry { Name = "gcr.io-proxy", HostPort = 5559, Proxy = new KSailMirrorRegistryProxy { Url = new Uri("https://gcr.io") } },
    new KSailMirrorRegistry { Name = "mcr.microsoft.com-proxy", HostPort = 5560, Proxy = new KSailMirrorRegistryProxy { Url = new Uri("https://mcr.microsoft.com") } },
    new KSailMirrorRegistry { Name = "quay.io-proxy", HostPort = 5561, Proxy = new KSailMirrorRegistryProxy { Url = new Uri("https://quay.io") } },
  ];

  [Description("Options for validating the KSail cluster.")]
  public KSailValidation Validation { get; set; } = new();

  public KSailClusterSpec() => SetOCISourceUri();

  public KSailClusterSpec(string name)
  {
    SetOCISourceUri();
    Connection = new KSailConnection
    {
      Context = $"kind-{name}"
    };
  }

  public KSailClusterSpec(string name, KSailKubernetesDistributionType distribution) : this(name)
  {
    SetOCISourceUri(distribution);
    Connection = new KSailConnection
    {
      Context = distribution switch
      {
        KSailKubernetesDistributionType.Native => $"kind-{name}",
        KSailKubernetesDistributionType.K3s => $"k3d-{name}",
        _ => $"kind-{name}"
      }
    };
    Project = new KSailProject
    {
      Distribution = distribution,
      DistributionConfigPath = distribution switch
      {
        KSailKubernetesDistributionType.Native => "kind-config.yaml",
        KSailKubernetesDistributionType.K3s => "k3d-config.yaml",
        _ => "kind-config.yaml"
      }
    };
  }

  void SetOCISourceUri(KSailKubernetesDistributionType distribution = KSailKubernetesDistributionType.Native)
  {
    DeploymentTool.Flux = distribution switch
    {
      KSailKubernetesDistributionType.Native => new KSailFluxDeploymentTool(new Uri("oci://ksail-registry:5000/ksail-registry")),
      KSailKubernetesDistributionType.K3s => new KSailFluxDeploymentTool(new Uri("oci://host.k3d.internal:5555/ksail-registry")),
      _ => new KSailFluxDeploymentTool(new Uri("oci://ksail-registry:5000/ksail-registry")),
    };
  }
}
