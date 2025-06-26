using System.CommandLine;
using KSail.Models;
using KSail.Models.Project.Enums;

namespace KSail.Options.Project;


class ProjectSecretManagerOption : Option<KSailSecretManagerType?>
{
  public ProjectSecretManagerOption(KSailCluster config) : base(
    "-sm", "--secret-manager"
  )
  {
    Description = "Whether to use a secret manager.";
    DefaultValueFactory = (result) => config.Spec.Project.SecretManager;
  }
}

