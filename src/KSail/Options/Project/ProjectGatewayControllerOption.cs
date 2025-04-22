using System.CommandLine;
using KSail.Models;
using KSail.Models.Project.Enums;

namespace KSail.Options.Project;


class ProjectGatewayControllerOption(KSailCluster config) : Option<KSailGatewayControllerType?>(
  ["-gc", "--gateway-controller"],
  $"The Gateway Controller to use. [default: {config.Spec.Project.GatewayController}]"
);
