using System.CommandLine;
using KSail.Models;

namespace KSail.Options.Project;

/// <summary>
/// The path to the ksail configuration file.
/// </summary>
public class ProjectConfigPathOption(KSailCluster config) : Option<string?>(
  ["--config", "-c"],
  $"The path to the ksail configuration file. [default: {config.Spec.Project.ConfigPath}]"
)
{ }
