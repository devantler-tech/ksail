using System.CommandLine;
using KSail.Models;

namespace KSail.Options.Project;


class ProjectConfigPathOption : Option<string?>
{
  public ProjectConfigPathOption(KSailCluster config) : base(
    "--config", "-c"
  )
  {
    Description = "The path to the ksail configuration file.";
    DefaultValueFactory = (result) => config.Spec.Project.ConfigPath;
  }
}

