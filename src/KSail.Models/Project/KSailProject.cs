using System.ComponentModel;
using KSail.Models.Project.Enums;
using YamlDotNet.Serialization;

namespace KSail.Models.Project;


public class KSailProject
{

  [Description("The path to the ksail configuration file. [default: ksail.yaml]")]
  public string ConfigPath { get; set; } = "ksail.yaml";

  [Description("The path to the distribution configuration file. [default: kind.yaml]")]
  public string DistributionConfigPath { get; set; } = "kind.yaml";

  [Description("The Kubernetes distribution to use. [default: Native]")]
  public KSailKubernetesDistributionType Distribution { get; set; } = KSailKubernetesDistributionType.Native;

  [Description("The Deployment tool to use. [default: Flux]")]
  public KSailDeploymentToolType DeploymentTool { get; set; } = KSailDeploymentToolType.Flux;

  [Description("The secret manager to use. [default: None]")]
  public KSailSecretManagerType SecretManager { get; set; } = KSailSecretManagerType.None;

  [Description("The CNI to use. [default: Default]")]
  [YamlMember(Alias = "cni")]
  public KSailCNIType CNI { get; set; } = KSailCNIType.Default;

  [Description("The editor to use for viewing files while debugging. [default: Nano]")]
  public KSailEditorType Editor { get; set; } = KSailEditorType.Nano;

  [Description("The engine to use for running the KSail cluster. [default: Docker]")]
  public KSailEngineType Engine { get; set; } = KSailEngineType.Docker;

  [Description("The path to the root kustomization directory. [default: k8s]")]
  public string KustomizationPath { get; set; } = "k8s";

  [Description("Whether to set up mirror registries for the project. [default: true]")]
  public bool MirrorRegistries { get; set; } = true;
}
