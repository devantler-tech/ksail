using KSail;
using KSail.Models;
using KSail.Models.Project.Enums;

namespace KSail.Managers;

class CSIManager(KSailCluster config) : IBootstrapper
{
  public Task BootstrapAsync(CancellationToken cancellationToken = default)
  {
    Console.WriteLine("💾 Bootstrapping CSI");
    switch (config.Spec.Project.CSI)
    {
      case KSailCSIType.Default:
        HandleDefaultCSI();
        break;
      default:
        throw new NotSupportedException($"the '{config.Spec.Project.CSI}' CSI is not supported.");
    }
    Console.WriteLine();
    return Task.CompletedTask;
  }

  void HandleDefaultCSI()
  {
    switch (config.Spec.Project.Provider, config.Spec.Project.Distribution)
    {
      case (KSailProviderType.Docker or KSailProviderType.Podman, KSailDistributionType.Native):
        Console.WriteLine("► Kind deploys the local-path-provisioner CSI by default");
        break;
      case (KSailProviderType.Docker or KSailProviderType.Podman, KSailDistributionType.K3s):
        Console.WriteLine("► K3d deploys the local-path-provisioner CSI by default");
        break;
      default:
        throw new NotSupportedException($"the '{config.Spec.Project.CSI}' CSI is not supported.");
    }
  }
}
