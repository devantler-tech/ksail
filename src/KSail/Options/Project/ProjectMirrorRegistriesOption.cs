using System.CommandLine;
using KSail.Models;
using KSail.Models.Project.Enums;

namespace KSail.Options.Project;


class ProjectMirrorRegistriesOption(KSailCluster config) : Option<bool?>
(
  ["-mr", "--mirror-registries"],
  $"Enable mirror registries for the project. [default: {config.Spec.Project.MirrorRegistries}]"
);
