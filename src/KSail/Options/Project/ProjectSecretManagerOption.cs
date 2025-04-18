using System.CommandLine;
using KSail.Models;
using KSail.Models.Project.Enums;

namespace KSail.Options.Project;


class ProjectSecretManagerOption(KSailCluster config) : Option<KSailSecretManagerType>(
  ["-sm", "--secret-manager"],
  $"Whether to use a secret manager. [default: {config.Spec.Project.SecretManager}]"
);
