using System.CommandLine;
using KSail.Models;
using KSail.Models.Project.Enums;

namespace KSail.Options.Project;


class ProjectIngressControllerOption(KSailCluster config) : Option<KSailIngressControllerType?>(
  ["-ic", "--ingress-controller"],
  $"The Ingress Controller to use. [default: {config.Spec.Project.IngressController}]"
);
