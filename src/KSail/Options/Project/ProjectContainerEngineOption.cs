using System.CommandLine;
using KSail.Models;
using KSail.Models.Project.Enums;

namespace KSail.Options.Project;


class ProjectContainerEngineOption(KSailCluster config) : Option<KSailContainerEngineType?>(
  ["-ce", "--container-engine"],
  $"The container engine in which to provision the cluster. [default: {config.Spec.Project.ContainerEngine}]"
);
