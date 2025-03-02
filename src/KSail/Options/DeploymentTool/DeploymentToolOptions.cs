using KSail.Models;

namespace KSail.Options.DeploymentTool;



class DeploymentToolOptions(KSailCluster config)
{

  public DeploymentToolFluxOptions Flux { get; set; } = new(config);

}
