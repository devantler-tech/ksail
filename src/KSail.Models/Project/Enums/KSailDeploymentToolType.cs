using System.ComponentModel;

namespace KSail.Models.Project.Enums;


public enum KSailDeploymentToolType
{
  [Description("Use Kubectl as the deployment tool.")]
  Kubectl,
  [Description("Use Flux GitOps as the deployment tool.")]
  Flux,
  [Description("Use ArgoCD as the deployment tool.")]
  ArgoCD
}
