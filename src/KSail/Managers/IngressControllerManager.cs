using System.Reflection.Metadata;
using System.Threading.Tasks;
using Devantler.HelmCLI;
using KSail;
using KSail.HelmInstallers;
using KSail.Models;
using KSail.Models.Project.Enums;

namespace KSail.Managers;

class IngressControllerManager(KSailCluster config) : IBootstrapManager
{
  readonly TraefikInstaller _traefikInstaller = new(config);

  public async Task BootstrapAsync(CancellationToken cancellationToken = default)
  {
    Console.WriteLine("🚦 Bootstrapping Ingress Controller");
    switch (config.Spec.Project.IngressController)
    {
      case KSailIngressControllerType.None:
        HandleNoneIngressController();
        break;
      case KSailIngressControllerType.Default:
        HandleDefaultIngressController();
        break;
      case KSailIngressControllerType.Traefik:
        await HandleTraefikIngressController(cancellationToken).ConfigureAwait(false);
        break;
      default:
        throw new NotSupportedException($"the '{config.Spec.Project.IngressController}' Ingress Controller is not supported.");
    }
    Console.WriteLine();
  }

  void HandleNoneIngressController()
  {
    switch (config.Spec.Project.Provider, config.Spec.Project.Distribution)
    {
      case (KSailProviderType.Docker or KSailProviderType.Podman, KSailDistributionType.Native):
        Console.WriteLine("► Kind does not deploy an Ingress Controller by default");
        break;
      case (KSailProviderType.Docker or KSailProviderType.Podman, KSailDistributionType.K3s):
        Console.WriteLine("✔ K3d Traefik Ingress Controller is disabled");
        break;
      default:
        break;
    }
  }

  void HandleDefaultIngressController()
  {
    switch (config.Spec.Project.Provider, config.Spec.Project.Distribution)
    {
      case (KSailProviderType.Docker or KSailProviderType.Podman, KSailDistributionType.Native):
        Console.WriteLine("► Kind does not deploy an Ingress Controller by default");
        break;
      case (KSailProviderType.Docker or KSailProviderType.Podman, KSailDistributionType.K3s):
        Console.WriteLine("✔ K3d Traefik Ingress Controller is enabled");
        break;
      default:
        break;
    }
  }

  async Task HandleTraefikIngressController(CancellationToken cancellationToken = default)
  {
    switch (config.Spec.Project.Provider, config.Spec.Project.Distribution)
    {
      case (KSailProviderType.Docker or KSailProviderType.Podman, KSailDistributionType.Native):
        Console.WriteLine("► Deploying Traefik Ingress Controller with Helm");
        await _traefikInstaller.AddRepositoryAsync(cancellationToken).ConfigureAwait(false);
        await _traefikInstaller.InstallAsync(cancellationToken).ConfigureAwait(false);
        Console.WriteLine("✔ Traefik Ingress Controller deployed");
        break;
      case (KSailProviderType.Docker or KSailProviderType.Podman, KSailDistributionType.K3s):
        Console.WriteLine("✔ K3d Traefik Ingress Controller is enabled");
        break;
      default:
        break;
    }
  }
}
