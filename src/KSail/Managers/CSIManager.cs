using KSail;
using KSail.Models;
using KSail.Models.Project.Enums;

namespace KSail.Managers;

class CSIManager(KSailCluster config) : IBootstrapManager
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
    switch (config.Spec.Project.Distribution)
    {
      case KSailDistributionType.Kind:
        Console.WriteLine("► Kind deploys the local-path-provisioner CSI by default");
        break;
      case KSailDistributionType.K3d:
        Console.WriteLine("► K3d deploys the local-path-provisioner CSI by default");
        break;
      default:
        throw new NotSupportedException($"the '{config.Spec.Project.CSI}' CSI is not supported.");
    }
  }
}
