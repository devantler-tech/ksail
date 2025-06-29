using System.CommandLine;
using KSail.Models;
using KSail.Models.Project.Enums;

namespace KSail.Options.Project;

class ProjectMetricsServerOption : Option<bool?>
{
  public ProjectMetricsServerOption(KSailCluster config) : base(
    "-ms", "--metrics-server"
  )
  {
    Description = "Whether to install Metrics Server.";
    DefaultValueFactory = (result) => config.Spec.Project.MetricsServer;
  }
}

