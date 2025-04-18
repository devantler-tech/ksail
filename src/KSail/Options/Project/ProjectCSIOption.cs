using System.CommandLine;
using KSail.Models;
using KSail.Models.Project.Enums;

namespace KSail.Options.Project;

class ProjectCSIOption(KSailCluster config) : Option<KSailCSIType>(
  ["--csi"],
  $"The CSI to use. [default: {config.Spec.Project.CSI}]"
);
