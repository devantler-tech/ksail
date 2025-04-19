
using Devantler.KubernetesProvisioner.CNI.Cilium;
using Devantler.KubernetesProvisioner.CNI.Core;
using KSail;
using KSail.Factories;
using KSail.Models;
using KSail.Models.Project.Enums;

class CNIBootstrapper(KSailCluster config) : IBootstrapper
{
  readonly CiliumProvisioner? _cniProvisioner = CNIProvisionerFactory.Create(config);
  public async Task BootstrapAsync(CancellationToken cancellationToken = default)
  {
    Console.WriteLine("🌐 Bootstrapping CNI");
    switch (config.Spec.Project.CNI)
    {
      case KSailCNIType.Cilium:
        await InstallCNI(cancellationToken).ConfigureAwait(false);
        break;
      case KSailCNIType.Default:
        HandleDefaultCNI();
        break;
      default:
        throw new KSailException($"the '{config.Spec.Project.CNI}' CNI is not supported.");
    }
    Console.WriteLine();
  }

  async Task InstallCNI(CancellationToken cancellationToken)
  {
    if (_cniProvisioner != null)
    {
      Console.WriteLine($"► installing '{config.Spec.Project.CNI}' CNI");
      await _cniProvisioner.InstallAsync(cancellationToken).ConfigureAwait(false);
    }
    Console.WriteLine($"✔ '{config.Spec.Project.CNI}' CNI installed");
  }

  void HandleDefaultCNI()
  {
    switch (config.Spec.Project.Provider, config.Spec.Project.Distribution)
    {
      case (KSailProviderType.Docker or KSailProviderType.Podman, KSailDistributionType.Native):
        Console.WriteLine("► Kind deploys the kindnetd CNI by default");
        break;
      case (KSailProviderType.Docker or KSailProviderType.Podman, KSailDistributionType.K3s):
        Console.WriteLine("► K3d deploys the Flannel CNI by default");
        break;
      default:
        break;
    }
  }
}
