using System.CommandLine;
using KSail.Models;
using KSail.Models.Project.Enums;

namespace KSail.Options.Project;

class ProjectCSIOption : Option<KSailCSIType?>
{
  public ProjectCSIOption(KSailCluster config) : base(
    "--csi"
  )
  {
    Description = "The CSI to use.";
    DefaultValueFactory = (result) => config.Spec.Project.CSI;
  }
}

