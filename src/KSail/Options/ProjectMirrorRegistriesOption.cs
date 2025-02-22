using System.CommandLine;
using KSail.Models;

namespace KSail.Options;

/// <summary>
/// Enable mirror registries.
/// </summary>
public class ProjectMirrorRegistriesOption(KSailCluster config) : Option<bool?>
(
  ["-mr", "--mirror-registries"],
  $"Enable mirror registries. Default: '{config.Spec.Project.MirrorRegistries}' (G)"
)
{
}
