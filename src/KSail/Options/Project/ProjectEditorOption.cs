using System.CommandLine;
using KSail.Models;
using KSail.Models.Project.Enums;

namespace KSail.Options.Project;


class ProjectEditorOption : Option<KSailEditorType?>
{
  public ProjectEditorOption(KSailCluster config) : base(
    "--editor", "-e"
  )
  {
    Description = "The editor to use for editing files from the CLI.";
    DefaultValueFactory = (result) => config.Spec.Project.Editor;
  }
}

