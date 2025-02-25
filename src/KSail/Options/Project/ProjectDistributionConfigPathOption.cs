using System.CommandLine;
using KSail.Models;

namespace KSail.Options.Project;

/// <summary>
/// The path to the distribution configuration file.
/// </summary>
/// <param name="config"></param>
public class ProjectDistributionConfigPathOption(KSailCluster config) : Option<string?>(
  ["--distribution-config", "-dc"],
  $"Path to the distribution configuration file. [default: {config.Spec.Project.DistributionConfigPath}]"
)
{ }
