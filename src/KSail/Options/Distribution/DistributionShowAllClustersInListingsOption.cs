using System.CommandLine;
using KSail.Models;

namespace KSail.Options.Distribution;



class DistributionShowAllClustersInListingsOption : Option<bool?>
{
  public DistributionShowAllClustersInListingsOption(KSailCluster config) : base(
    "--all", "-a"
  )
  {
    Description = "List clusters from all distributions.";
    DefaultValueFactory = (result) => config.Spec.Distribution.ShowAllClustersInListings;
  }
}

