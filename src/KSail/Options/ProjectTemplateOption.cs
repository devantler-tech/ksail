using System.CommandLine;
using KSail.Models;
using KSail.Models.Project;

namespace KSail.Options;

/// <summary>
/// The project template
/// </summary>
public class ProjectTemplateOption(KSailCluster config) : Option<KSailProjectTemplate>
(
  ["-t", "--template"],
  $"The template to use for the initialized cluster. Default: '{config.Spec.Project.Template}' (G)"
)
{
}
