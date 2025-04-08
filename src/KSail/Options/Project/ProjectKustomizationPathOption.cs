using System.CommandLine;
using KSail.Models;

namespace KSail.Options.Project;

class ProjectKustomizationPathOption(KSailCluster config) : Option<string?>(
  ["--kustomization-path", "-kp"],
  $"The path to the root kustomization directory. [default: {config.Spec.Project.KustomizationPath}]"
);
