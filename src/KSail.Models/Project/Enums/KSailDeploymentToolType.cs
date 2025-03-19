using System.ComponentModel;

namespace KSail.Models.Project.Enums;


public enum KSailDeploymentToolType
{
  [Description("Use Flux GitOps to deploy manifests.")]
  Flux

  //
  // ArgoCD

  //
  // Kubectl
}
