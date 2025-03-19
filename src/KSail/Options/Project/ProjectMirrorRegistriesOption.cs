using System.CommandLine;
using KSail.Models;
using KSail.Models.Project.Enums;

namespace KSail.Options.Project;


class ProjectMirrorRegistriesOption(KSailCluster config) : Option<KSailMirrorRegistriesType?>
(
  ["-mr", "--mirror-registries"],
  $"Configure how to handle mirror registries. [default: {config.Spec.Project.MirrorRegistries}]"
)
{
}
