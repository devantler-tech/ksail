using System.CommandLine;
using KSail.Models;
using KSail.Models.Project;

namespace KSail.Options;

/// <summary>
/// The distribution to use for the cluster.
/// </summary>
public class ProjectDistributionOption(KSailCluster config)
 : Option<KSailKubernetesDistribution>(
    ["-d", "--distribution"],
    $"The distribution to use for the cluster. Default: '{config.Spec.Project.Distribution}' (G)"
  )
{
}
