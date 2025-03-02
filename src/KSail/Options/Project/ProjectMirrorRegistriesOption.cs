using System.CommandLine;
using KSail.Models;

namespace KSail.Options.Project;


class ProjectMirrorRegistriesOption(KSailCluster config) : Option<bool?>
(
  ["-mr", "--mirror-registries"],
  $"Enable mirror registries. [default: {config.Spec.Project.MirrorRegistries}]"
)
{
}
