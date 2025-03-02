using KSail.Models;

namespace KSail.Options.Distribution;



class DistributionOptions(KSailCluster config)
{

  public DistributionShowAllClustersInListingsOption ShowAllClustersInListings { get; set; } = new(config);
}
