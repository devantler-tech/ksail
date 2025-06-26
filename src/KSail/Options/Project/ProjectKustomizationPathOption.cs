using System.CommandLine;
using KSail.Models;

namespace KSail.Options.Project;

class ProjectKustomizationPathOption : Option<string?>
{
  public ProjectKustomizationPathOption(KSailCluster config) : base(
    "--kustomization-path", "-kp"
  )
  {
    Description = "The path to the root kustomization directory.";
    DefaultValueFactory = (result) => config.Spec.Project.KustomizationPath;
  }
}

