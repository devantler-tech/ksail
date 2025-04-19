using KSail;
using KSail.Models;
using KSail.Models.Project.Enums;

class GatewayControllerBootstrapper(KSailCluster config) : IBootstrapper
{
  public Task BootstrapAsync(CancellationToken cancellationToken = default)
  {
    Console.WriteLine("🚦🆕 Bootstrapping Gateway Controller");
    switch (config.Spec.Project.GatewayController)
    {
      case KSailGatewayControllerType.Default:
        HandleDefaultGatewayController();
        break;
      default:
        throw new KSailException($"the '{config.Spec.Project.GatewayController}' Gateway Controller is not supported.");
    }
    Console.WriteLine();
    return Task.CompletedTask;
  }

  void HandleDefaultGatewayController()
  {
    switch (config.Spec.Project.Provider, config.Spec.Project.Distribution)
    {
      case (KSailProviderType.Docker or KSailProviderType.Podman, KSailDistributionType.Native):
        Console.WriteLine("► Kind does not deploy a Gateway Controller by default");
        break;
      case (KSailProviderType.Docker or KSailProviderType.Podman, KSailDistributionType.K3s):
        Console.WriteLine("► K3d does not deploy a Gateway Controller by default");
        break;
      default:
        break;
    }
  }
}
