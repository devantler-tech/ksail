using System.CommandLine;
using KSail.Models;
using KSail.Models.Project;

namespace KSail.Options;

/// <summary>
/// The secret manager to use for the cluster.
/// </summary>
public class ProjectSecretManagerOption(KSailCluster config) : Option<KSailSecretManager>(
  ["-sm", "--secret-manager"],
  $"Configure which secret manager to use. Default: '{config.Spec.Project.SecretManager}' (G)"
)
{
}
