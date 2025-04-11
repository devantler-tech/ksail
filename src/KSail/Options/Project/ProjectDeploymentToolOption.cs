using System.CommandLine;
using KSail.Models;
using KSail.Models.Project.Enums;

namespace KSail.Options.Project;



class ProjectDeploymentToolOption(KSailCluster config) : Option<KSailDeploymentToolType>(
  ["-dt", "--deployment-tool"],
  $"The Deployment tool to use for applying a kustomization. [default: {config.Spec.Project.DeploymentTool}]"
);
