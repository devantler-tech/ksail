using System.CommandLine;
using KSail.Models;

namespace KSail.Options.Metadata;


class MetadataNameOption(KSailCluster config) : Option<string>(
  ["-n", "--name"],
  $"The name of the cluster. [default: {config.Metadata.Name}]"
)
{
}
