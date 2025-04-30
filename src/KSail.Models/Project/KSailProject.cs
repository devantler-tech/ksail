using System.ComponentModel;
using KSail.Models.GatewayController;
using KSail.Models.Project.Enums;
using YamlDotNet.Serialization;

namespace KSail.Models.Project;


public class KSailProject
{

  [Description("The path to the ksail configuration file. [default: ksail.yaml]")]
  public string ConfigPath { get; set; } = "ksail.yaml";

  [Description("The path to the distribution configuration file. [default: kind.yaml]")]
  public string DistributionConfigPath { get; set; } = "kind.yaml";

  [Description("The path to the root kustomization directory. [default: k8s]")]
  public string KustomizationPath { get; set; } = "k8s";

  [Description("The provider to use for running the KSail cluster. [default: Docker]")]
  public KSailContainerEngineType ContainerEngine { get; set; } = KSailContainerEngineType.Docker;

  [Description("The Kubernetes distribution to use. [default: Kind]")]
  public KSailDistributionType Distribution { get; set; } = KSailDistributionType.Kind;

  [Description("The Deployment tool to use. [default: Kubectl]")]
  public KSailDeploymentToolType DeploymentTool { get; set; } = KSailDeploymentToolType.Kubectl;

  [Description("The CNI to use. [default: Default]")]
  [YamlMember(Alias = "cni")]
  public KSailCNIType CNI { get; set; } = KSailCNIType.Default;

  [Description("The CSI to use. [default: Default]")]
  [YamlMember(Alias = "csi")]
  public KSailCSIType CSI { get; set; } = KSailCSIType.Default;

  [Description("The Ingress Controller to use. [default: Default]")]
  public KSailIngressControllerType IngressController { get; set; } = KSailIngressControllerType.Default;

  [Description("The Gateway Controller to use. [default: Default]")]
  public KSailGatewayControllerType GatewayController { get; set; } = KSailGatewayControllerType.Default;

  [Description("Whether to install Metrics Server. [default: true]")]
  public bool MetricsServer { get; set; } = true;

  [Description("Whether to use a secret manager. [default: None]")]
  public KSailSecretManagerType SecretManager { get; set; } = KSailSecretManagerType.None;

  [Description("The editor to use for viewing files while debugging. [default: Nano]")]
  public KSailEditorType Editor { get; set; } = KSailEditorType.Nano;

  [Description("Whether to set up mirror registries for the project. [default: true]")]
  public bool MirrorRegistries { get; set; } = true;
}
