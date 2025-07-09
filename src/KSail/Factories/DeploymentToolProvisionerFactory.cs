using DevantlerTech.KubernetesProvisioner.Deployment.Core;
using DevantlerTech.KubernetesProvisioner.Deployment.Kubectl;
using DevantlerTech.KubernetesProvisioner.GitOps.Flux;
using KSail.Models;
using KSail.Models.Project.Enums;

namespace KSail.Factories;

class DeploymentToolProvisionerFactory
{
  internal static IDeploymentToolProvisioner Create(KSailCluster config)
  {
    string scheme = config.Spec.DeploymentTool.Flux.Source.Url.Scheme;
    string host = "localhost";
    int port = config.Spec.LocalRegistry.HostPort;
    string absolutePath = config.Spec.DeploymentTool.Flux.Source.Url.AbsolutePath;
    var _ociRegistryFromHost = new Uri($"{scheme}://{host}:{port}{absolutePath}");
    return config.Spec.Project.DeploymentTool switch
    {
      KSailDeploymentToolType.Kubectl => new KubectlProvisioner(config.Spec.Connection.Kubeconfig, config.Spec.Connection.Context),
      KSailDeploymentToolType.Flux => new FluxProvisioner(_ociRegistryFromHost, config.Spec.Connection.Kubeconfig, config.Spec.Connection.Context),
      KSailDeploymentToolType.ArgoCD => new ArgoCDProvisioner(_ociRegistryFromHost, config.Spec.Connection.Kubeconfig, config.Spec.Connection.Context),
      _ => throw new NotSupportedException($"The Deployment tool '{config.Spec.Project.DeploymentTool}' is not supported.")
    };
  }
}
