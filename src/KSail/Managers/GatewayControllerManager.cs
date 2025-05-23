using KSail;
using KSail.Models;
using KSail.Models.Project.Enums;

namespace KSail.Managers;

class GatewayControllerManager(KSailCluster config) : IBootstrapManager
{
  public Task BootstrapAsync(CancellationToken cancellationToken = default)
  {
    Console.WriteLine("🚦🆕 Bootstrapping Gateway Controller");
    switch (config.Spec.Project.GatewayController)
    {
      case KSailGatewayControllerType.None or KSailGatewayControllerType.Default:
        HandleDefaultGatewayController();
        break;
      default:
        throw new NotSupportedException($"the '{config.Spec.Project.GatewayController}' Gateway Controller is not supported.");
    }
    Console.WriteLine();
    return Task.CompletedTask;
  }

  void HandleDefaultGatewayController()
  {
    switch (config.Spec.Project.Distribution)
    {
      case KSailDistributionType.Kind:
        Console.WriteLine("► Kind does not deploy a Gateway Controller by default");
        break;
      case KSailDistributionType.K3d:
        Console.WriteLine("► K3d does not deploy a Gateway Controller by default");
        break;
      default:
        break;
    }
  }
}
