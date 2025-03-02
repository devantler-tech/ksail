using System.CommandLine;
using KSail.Models;

namespace KSail.Options.Distribution;



class DistributionShowAllClustersInListingsOption(KSailCluster config) : Option<bool?>(
  ["--all", "-a"],
  $"List clusters from all distributions. [default: {config.Spec.Distribution.ShowAllClustersInListings}]"
);
