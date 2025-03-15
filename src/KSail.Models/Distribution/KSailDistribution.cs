using System.ComponentModel;

namespace KSail.Models.Distribution;


public class KSailDistribution
{

  [Description("Show clusters from all supported distributions. [default: false]")]
  public bool ShowAllClustersInListings { get; set; }
}
