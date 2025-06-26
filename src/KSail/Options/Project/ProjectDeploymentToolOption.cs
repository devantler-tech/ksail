using System.CommandLine;
using KSail.Models;
using KSail.Models.Project.Enums;

namespace KSail.Options.Project;



class ProjectDeploymentToolOption : Option<KSailDeploymentToolType?>
{
  public ProjectDeploymentToolOption(KSailCluster config) : base(
    "-dt", "--deployment-tool"
  )
  {
    Description = "The Deployment tool to use for applying a kustomization.";
    DefaultValueFactory = (result) => config.Spec.Project.DeploymentTool;
  }
}

