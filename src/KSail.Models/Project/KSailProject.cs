using System.ComponentModel;
using Devantler.K9sCLI;
using YamlDotNet.Serialization;

namespace KSail.Models.Project;

/// <summary>
/// The options for the KSail project.
/// </summary>
public class KSailProject
{
  /// <summary>
  /// The working directory for the project.
  /// </summary>
  [Description("The working directory for the project.")]
  public string WorkingDirectory { get; set; } = ".";

  /// <summary>
  /// The path to the ksail configuration file.
  /// </summary>
  [Description("The path to the ksail configuration file.")]
  public string ConfigPath { get; set; } = "ksail-config.yaml";

  /// <summary>
  /// The path to the distribution configuration file.
  /// </summary>
  [Description("The path to the distribution configuration file.")]
  public string DistributionConfigPath { get; set; } = "kind-config.yaml";

  /// <summary>
  /// The template used for the project.
  /// </summary>
  [Description("The template used for the project.")]
  public KSailProjectTemplate Template { get; set; } = KSailProjectTemplate.Kustomize;

  /// <summary>
  /// The container engine to use.
  /// </summary>
  [Description("The engine to use for running the KSail cluster.")]
  public KSailEngine Engine { get; set; } = KSailEngine.Docker;

  /// <summary>
  /// The Kubernetes distribution to use.
  /// </summary>
  [Description("The Kubernetes distribution to use.")]
  public KSailKubernetesDistribution Distribution { get; set; } = KSailKubernetesDistribution.Native;


  /// <summary>
  /// The Deployment tool to use.
  /// </summary>
  [Description("The Deployment tool to use.")]
  public KSailDeploymentTool DeploymentTool { get; set; } = KSailDeploymentTool.Flux;

  /// <summary>
  /// The secret manager to use.
  /// </summary>
  [Description("The secret manager to use.")]
  public KSailSecretManager SecretManager { get; set; } = KSailSecretManager.None;

  /// <summary>
  /// The CNI to use.
  /// </summary>
  [Description("The CNI to use.")]
  [YamlMember(Alias = "cni")]
  public KSailCNI CNI { get; set; } = KSailCNI.Default;

  /// <summary>
  /// The editor to use for viewing files while debugging.
  /// </summary>
  [Description("The editor to use for viewing files while debugging.")]
  public Editor Editor { get; set; } = Editor.Nano;

  /// <summary>
  /// Whether to set up mirror registries for the project.
  /// </summary>
  [Description("Whether to set up mirror registries for the project.")]
  public bool MirrorRegistries { get; set; } = true;
}
