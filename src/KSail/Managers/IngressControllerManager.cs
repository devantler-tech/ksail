using System.Reflection.Metadata;
using System.Threading.Tasks;
using DevantlerTech.HelmCLI;
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
    switch (config.Spec.Project.Distribution)
    {
      case KSailDistributionType.Kind:
        Console.WriteLine("► Kind does not deploy an Ingress Controller by default");
        break;
      case KSailDistributionType.K3d:
        Console.WriteLine("✔ K3d Traefik Ingress Controller is disabled");
        break;
      default:
        break;
    }
  }

  void HandleDefaultIngressController()
  {
    switch (config.Spec.Project.Distribution)
    {
      case KSailDistributionType.Kind:
        Console.WriteLine("► Kind does not deploy an Ingress Controller by default");
        break;
      case KSailDistributionType.K3d:
        Console.WriteLine("✔ K3d Traefik Ingress Controller is enabled");
        break;
      default:
        break;
    }
  }

  async Task HandleTraefikIngressController(CancellationToken cancellationToken = default)
  {
    switch (config.Spec.Project.Distribution)
    {
      case KSailDistributionType.Kind:
        Console.WriteLine("► Installing Traefik Ingress Controller with Helm");
        await _traefikInstaller.AddRepositoryAsync(cancellationToken).ConfigureAwait(false);
        await _traefikInstaller.InstallAsync(cancellationToken).ConfigureAwait(false);
        Console.WriteLine("✔ Installed Traefik Ingress Controller with Helm");
        break;
      case KSailDistributionType.K3d:
        Console.WriteLine("✔ K3d Traefik Ingress Controller is enabled");
        break;
      default:
        break;
    }
  }
}
