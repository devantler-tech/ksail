using KSail;
using KSail.Models;
using KSail.Models.Project.Enums;

class IngressControllerBootstrapper(KSailCluster config) : IBootstrapper
{
  public Task BootstrapAsync(CancellationToken cancellationToken = default)
  {
    Console.WriteLine("🚦 Bootstrapping Ingress Controller");
    switch (config.Spec.Project.IngressController)
    {
      case KSailIngressControllerType.Default:
        HandleDefaultIngressController();
        break;
      default:
        throw new NotSupportedException($"the '{config.Spec.Project.IngressController}' Ingress Controller is not supported.");
    }
    Console.WriteLine();
    return Task.CompletedTask;
  }

  void HandleDefaultIngressController()
  {
    switch (config.Spec.Project.Provider, config.Spec.Project.Distribution)
    {
      case (KSailProviderType.Docker or KSailProviderType.Podman, KSailDistributionType.Native):
        Console.WriteLine("► Kind does not deploy an Ingress Controller by default");
        break;
      case (KSailProviderType.Docker or KSailProviderType.Podman, KSailDistributionType.K3s):
        Console.WriteLine("► K3d deploys the Traefik Ingress Controller by default");
        break;
      default:
        break;
    }
  }
}
