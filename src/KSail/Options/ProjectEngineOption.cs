using System.CommandLine;
using KSail.Models;
using KSail.Models.Project;

namespace KSail.Options;

/// <summary>
/// The engine to use for provisioning the cluster.
/// </summary>
public class ProjectEngineOption(KSailCluster config)
 : Option<KSailEngine>(
    ["-e", "--engine"],
    $"The engine to use for provisioning the cluster. Default: '{config.Spec.Project.Engine}' (G)"
  )
{
}
