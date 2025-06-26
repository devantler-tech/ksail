using System.CommandLine;
using KSail.Models;
using KSail.Models.Project.Enums;

namespace KSail.Options.Project;


class ProjectIngressControllerOption : Option<KSailIngressControllerType?>
{
  public ProjectIngressControllerOption(KSailCluster config) : base(
    "-ic", "--ingress-controller"
  )
  {
    Description = $"The Ingress Controller to use.";
    DefaultValueFactory = (result) => config.Spec.Project.IngressController;
  }
}

