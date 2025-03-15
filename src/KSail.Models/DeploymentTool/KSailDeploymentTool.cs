using System.ComponentModel;

namespace KSail.Models.DeploymentTool;


public class KSailDeploymentTool
{
  [Description("The options for the Flux deployment tool.")]
  public KSailFluxDeploymentTool Flux { get; set; } = new();
}
