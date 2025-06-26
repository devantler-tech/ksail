using System.CommandLine;
using KSail.Models;
using KSail.Models.Project.Enums;

namespace KSail.Options.Project;


class ProjectContainerEngineOption : Option<KSailContainerEngineType?>
{
  public ProjectContainerEngineOption(KSailCluster config) : base(
    "-ce", "--container-engine"
  )
  {
    Description = "The container engine in which to provision the cluster.";
    DefaultValueFactory = (result) => config.Spec.Project.ContainerEngine;
  }
}

