using System.CommandLine;
using KSail.Models;

namespace KSail.Options.DeploymentTool;


class DeploymentToolFluxSourceUrlOption : Option<Uri?>
{
  public DeploymentToolFluxSourceUrlOption(KSailCluster config) : base(
   "--flux-source-url", "-fsu"
  )
  {
    Description = "Flux source URL for reconciling GitOps resources.";
    DefaultValueFactory = (result) => config.Spec.DeploymentTool.Flux.Source.Url;
  }
}

