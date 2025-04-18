using System.Text;
using Devantler.ContainerEngineProvisioner.Docker;
using Devantler.KubernetesProvisioner.Cluster.Core;
using Devantler.KubernetesProvisioner.CNI.Cilium;
using Devantler.KubernetesProvisioner.Deployment.Core;
using Devantler.KubernetesProvisioner.GitOps.Core;
using Devantler.KubernetesProvisioner.Resources.Native;
using Devantler.SecretManager.SOPS.LocalAge;
using k8s;
using k8s.Models;
using KSail.Commands.Validate.Handlers;
using KSail.Factories;
using KSail.Models;
using KSail.Models.MirrorRegistry;
using KSail.Models.Project.Enums;
using KSail.Utils;

namespace KSail.Commands.Up.Handlers;

class KSailUpCommandHandler(KSailCluster config)
{
  readonly ClusterBootstrapper _clusterBootstrapper = new(config);
  readonly GitOpsSourceBootstrapper _gitOpsSourceBootstrapper = new(config);
  readonly MirrorRegistryBootstrapper _mirrorRegistryBootstrapper = new(config);
  readonly CNIBootstrapper _cniBootstrapper = new(config);
  readonly CSIBootstrapper _csiBootstrapper = new(config);
  readonly IngressControllerBootstrapper _ingressControllerBootstrapper = new(config);
  readonly GatewayControllerBootstrapper _gatewayControllerBootstrapper = new(config);
  readonly SecretManagerBootstrapper _secretManagerBootstrapper = new(config);
  readonly DeploymentToolBootstrapper _deploymentToolBootstrapper = new(config);
  internal async Task<int> HandleAsync(CancellationToken cancellationToken = default)
  {
    await _clusterBootstrapper.BootstrapAsync(cancellationToken).ConfigureAwait(false);
    await _gitOpsSourceBootstrapper.BootstrapAsync(cancellationToken).ConfigureAwait(false);
    await _mirrorRegistryBootstrapper.BootstrapAsync(cancellationToken).ConfigureAwait(false);
    await _cniBootstrapper.BootstrapAsync(cancellationToken).ConfigureAwait(false);
    await _csiBootstrapper.BootstrapAsync(cancellationToken).ConfigureAwait(false);
    await _ingressControllerBootstrapper.BootstrapAsync(cancellationToken).ConfigureAwait(false);
    await _gatewayControllerBootstrapper.BootstrapAsync(cancellationToken).ConfigureAwait(false);
    await _secretManagerBootstrapper.BootstrapAsync(cancellationToken).ConfigureAwait(false);
    await _deploymentToolBootstrapper.BootstrapAsync(cancellationToken).ConfigureAwait(false);
    return 0;
  }
}
