using System.CommandLine;
using KSail.Models;
using KSail.Models.Project.Enums;

namespace KSail.Options.Project;

class ProjectGatewayControllerOption : Option<KSailGatewayControllerType?>
{
  public ProjectGatewayControllerOption(KSailCluster config) : base(
    "-gc", "--gateway-controller"
  )
  {
    Description = "The Gateway Controller to use.";
    DefaultValueFactory = (result) => config.Spec.Project.GatewayController;
  }
}
