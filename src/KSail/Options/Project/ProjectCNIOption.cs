using System.CommandLine;
using KSail.Models;
using KSail.Models.Project.Enums;

namespace KSail.Options.Project;

class ProjectCNIOption(KSailCluster config) : Option<KSailCNIType>(
  ["--cni"],
  $"The CNI to use. [default: {config.Spec.Project.CNI}]"
)
{ }
