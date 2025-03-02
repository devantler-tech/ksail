using System.CommandLine;
using KSail.Models;
using KSail.Models.Project.Enums;

namespace KSail.Options.Project;


class ProjectSecretManagerOption(KSailCluster config) : Option<KSailSecretManagerType>(
  ["-sm", "--secret-manager"],
  $"Configure which secret manager to use. [default: {config.Spec.Project.SecretManager}]"
)
{
}
