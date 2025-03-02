using System.ComponentModel;
using KSail.Models.Project.Enums;
using YamlDotNet.Serialization;

namespace KSail.Models.Project;


public class KSailProject
{

  [Description("The path to the ksail configuration file.")]
  public string ConfigPath { get; set; } = "ksail-config.yaml";

  [Description("The path to the distribution configuration file.")]
  public string DistributionConfigPath { get; set; } = "kind-config.yaml";

  [Description("The Kubernetes distribution to use.")]
  public KSailKubernetesDistributionType Distribution { get; set; } = KSailKubernetesDistributionType.Native;

  [Description("The Deployment tool to use.")]
  public KSailDeploymentToolType DeploymentTool { get; set; } = KSailDeploymentToolType.Flux;

  [Description("The secret manager to use.")]
  public KSailSecretManagerType SecretManager { get; set; } = KSailSecretManagerType.None;

  [Description("The CNI to use.")]
  [YamlMember(Alias = "cni")]
  public KSailCNIType CNI { get; set; } = KSailCNIType.Default;

  [Description("The editor to use for viewing files while debugging.")]
  public KSailEditorType Editor { get; set; } = KSailEditorType.Nano;

  [Description("The engine to use for running the KSail cluster.")]
  public KSailEngineType Engine { get; set; } = KSailEngineType.Docker;

  [Description("The path to the root kustomization directory.")]
  public string KustomizationPath { get; set; } = "k8s";

  [Description("Whether to set up mirror registries for the project.")]
  public bool MirrorRegistries { get; set; } = true;
}
