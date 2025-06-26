using System.Text;
using DevantlerTech.ContainerEngineProvisioner.Docker;
using DevantlerTech.KubernetesProvisioner.Cluster.Core;
using DevantlerTech.KubernetesProvisioner.CNI.Cilium;
using DevantlerTech.KubernetesProvisioner.Deployment.Core;
using DevantlerTech.KubernetesProvisioner.GitOps.Core;
using DevantlerTech.KubernetesProvisioner.Resources.Native;
using DevantlerTech.SecretManager.SOPS.LocalAge;
using k8s;
using k8s.Models;
using KSail.Commands.Validate.Handlers;
using KSail.Factories;
using KSail.Managers;
using KSail.Models;
using KSail.Models.MirrorRegistry;
using KSail.Models.Project.Enums;
using KSail.Utils;

namespace KSail.Commands.Up.Handlers;

class KSailUpCommandHandler(KSailCluster config) : ICommandHandler
{
  readonly ClusterManager _clusterManager = new(config);
  readonly GitOpsSourceManager _gitOpsSourceManager = new(config);
  readonly MirrorRegistryManager _mirrorRegistryManager = new(config);
  readonly CNIManager _cniManager = new(config);
  readonly CSIManager _csiManager = new(config);
  readonly IngressControllerManager _ingressControllerManager = new(config);
  readonly GatewayControllerManager _gatewayControllerManager = new(config);
  readonly MetricsServerManager _metricsServerManager = new(config);
  readonly SecretManager _secretManagerManager = new(config);
  readonly DeploymentToolManager _deploymentToolManager = new(config);
  public async Task HandleAsync(CancellationToken cancellationToken = default)
  {
    await _clusterManager.BootstrapAsync(cancellationToken).ConfigureAwait(false);
    await _gitOpsSourceManager.BootstrapAsync(cancellationToken).ConfigureAwait(false);
    await _mirrorRegistryManager.BootstrapAsync(cancellationToken).ConfigureAwait(false);
    await _cniManager.BootstrapAsync(cancellationToken).ConfigureAwait(false);
    await _csiManager.BootstrapAsync(cancellationToken).ConfigureAwait(false);
    if (config.Spec.Project.CNI != KSailCNIType.None)
    {
      await _ingressControllerManager.BootstrapAsync(cancellationToken).ConfigureAwait(false);
      await _gatewayControllerManager.BootstrapAsync(cancellationToken).ConfigureAwait(false);
      await _metricsServerManager.BootstrapAsync(cancellationToken).ConfigureAwait(false);
    }
    await _secretManagerManager.BootstrapAsync(cancellationToken).ConfigureAwait(false);
    await _deploymentToolManager.BootstrapAsync(cancellationToken).ConfigureAwait(false);
  }
}
