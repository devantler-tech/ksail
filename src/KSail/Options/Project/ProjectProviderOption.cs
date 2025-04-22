using System.CommandLine;
using KSail.Models;
using KSail.Models.Project.Enums;

namespace KSail.Options.Project;


class ProjectProviderOption(KSailCluster config) : Option<KSailProviderType?>(
  ["-p", "--provider"],
  $"The provider to use for provisioning the cluster. [default: {config.Spec.Project.Provider}]"
);
