using System.CommandLine;
using KSail.Models;
using KSail.Models.Project.Enums;

namespace KSail.Options.Project;

class ProjectCNIOption : Option<KSailCNIType?>
{
  public ProjectCNIOption(KSailCluster config) : base(
    "--cni"
  )
  {
    Description = "The CNI to use.";
    DefaultValueFactory = (result) => config.Spec.Project.CNI;
  }
}

