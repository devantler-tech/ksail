using System.CommandLine;
using KSail.Models;
using KSail.Models.Project.Enums;

namespace KSail.Options.Project;


class ProjectDistributionOption(KSailCluster config)
 : Option<KSailKubernetesDistributionType>(
    ["-d", "--distribution"],
    $"The distribution to use for the cluster. [default: {config.Spec.Project.Distribution}]"
  )
{
}
