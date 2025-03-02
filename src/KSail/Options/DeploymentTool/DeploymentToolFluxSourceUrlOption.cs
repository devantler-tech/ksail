using System.CommandLine;
using KSail.Models;

namespace KSail.Options.DeploymentTool;


class DeploymentToolFluxSourceUrlOption(KSailCluster config) : Option<string>(
 ["--flux-source-url", "-fsu"],
  $"Flux source URL for reconciling GitOps resources. [default: {config.Spec.DeploymentTool.Flux.Source.Url}]"
);
