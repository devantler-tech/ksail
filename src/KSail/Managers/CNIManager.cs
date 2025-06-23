using DevantlerTech.KubernetesProvisioner.CNI.Cilium;
using DevantlerTech.KubernetesProvisioner.CNI.Core;
using KSail;
using KSail.Factories;
using KSail.Models;
using KSail.Models.Project.Enums;

namespace KSail.Managers;

class CNIManager(KSailCluster config) : IBootstrapManager
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
      case KSailCNIType.None:
        HandleNoCNI();
        break;
      default:
        throw new NotSupportedException($"the '{config.Spec.Project.CNI}' CNI is not supported.");
    }
    Console.WriteLine();
  }

  void HandleNoCNI()
  {
    switch (config.Spec.Project.Distribution)
    {
      case KSailDistributionType.Kind:
        Console.WriteLine("✔ kindnetd CNI disabled");
        break;
      case KSailDistributionType.K3d:
        Console.WriteLine("✔ Flannel CNI disabled");
        break;
      default:
        break;
    }
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
    switch (config.Spec.Project.Distribution)
    {
      case KSailDistributionType.Kind:
        Console.WriteLine("► Kind deploys the kindnetd CNI by default");
        break;
      case KSailDistributionType.K3d:
        Console.WriteLine("► K3d deploys the Flannel CNI by default");
        break;
      default:
        break;
    }
  }
}
