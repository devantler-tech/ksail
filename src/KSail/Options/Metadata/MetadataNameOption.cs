using System.CommandLine;
using KSail.Models;

namespace KSail.Options.Metadata;


class MetadataNameOption : Option<string?>
{
  public MetadataNameOption(KSailCluster config) : base(
    "-n", "--name"
  )
  {
    Description = "The name of the cluster.";
    DefaultValueFactory = (result) => config.Metadata.Name;
  }
}

