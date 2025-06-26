using System.CommandLine;
using KSail.Models;
using KSail.Models.Project.Enums;

namespace KSail.Options.Project;


class ProjectDistributionOption : Option<KSailDistributionType?>
{
  public ProjectDistributionOption(KSailCluster config) : base(
    "-d", "--distribution"
  )
  {
    Description = "The distribution to use for the cluster.";
    DefaultValueFactory = (result) => config.Spec.Project.Distribution;
  }
}

