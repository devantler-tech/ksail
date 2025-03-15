using System.ComponentModel;

namespace KSail.Models.DeploymentTool;


public class KSailFluxDeploymentTool
{

  [Description("The source for reconciling GitOps resources.")]
  public KSailFluxDeploymentToolRepository Source { get; set; } = new KSailFluxDeploymentToolRepository();
}
