using System.CommandLine;
using KSail.Models;
using KSail.Models.Project.Enums;

namespace KSail.Options.Project;


class ProjectMirrorRegistriesOption : Option<bool?>
{
  public ProjectMirrorRegistriesOption(KSailCluster config) : base(
    "-mr", "--mirror-registries"
  )
  {
    Description = "Enable mirror registries for the project.";
    DefaultValueFactory = (result) => config.Spec.Project.MirrorRegistries;
  }
}

