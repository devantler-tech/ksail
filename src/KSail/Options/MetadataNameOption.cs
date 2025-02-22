using System.CommandLine;
using KSail.Models;

namespace KSail.Options;

/// <summary>
/// The name of the cluster.
/// </summary>
public class MetadataNameOption(KSailCluster config) : Option<string>(
  ["-n", "--name"],
  $"The name of the cluster. Default: '{config.Metadata.Name}' (G)"
)
{
}
