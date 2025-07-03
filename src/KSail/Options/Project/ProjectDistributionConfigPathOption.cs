using System.CommandLine;
using KSail.Models;

namespace KSail.Options.Project;



class ProjectDistributionConfigPathOption : Option<string?>
{
  public ProjectDistributionConfigPathOption(KSailCluster config) : base(
    "--distribution-config", "-dc"
  )
  {
    Description = "The path to the distribution configuration file.";
    DefaultValueFactory = (result) => config.Spec.Project.DistributionConfigPath;
  }
}

