using System.ComponentModel;

namespace KSail.Models.Project.Enums;


public enum KSailDeploymentToolType
{
  [Description("Use Kubectl to apply a kustomization.")]
  Kubectl,
  [Description("Use Flux GitOps to apply a kustomization.")]
  Flux

  // ArgoCD
}
