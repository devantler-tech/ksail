using System.Reflection.Metadata;
using System.Threading.Tasks;
using Devantler.HelmCLI;
using KSail;
using KSail.HelmInstallers;
using KSail.Models;
using KSail.Models.Project.Enums;

namespace KSail.Managers;

class MetricsServerManager(KSailCluster config) : IBootstrapManager
{
  readonly MetricsServerInstaller _metricsServerInstaller = new(config);

  public async Task BootstrapAsync(CancellationToken cancellationToken = default)
  {
    Console.WriteLine("📊 Bootstrapping Metrics Server");
    if (config.Spec.Project.MetricsServer)
    {
      await HandleMetricsServer(cancellationToken).ConfigureAwait(false);
    }
    else
    {
      HandleNoMetricsServer();
    }
    Console.WriteLine();
  }

  async Task HandleMetricsServer(CancellationToken cancellationToken = default)
  {
    switch (config.Spec.Project.Distribution)
    {
      case KSailDistributionType.Kind:
        Console.WriteLine("► Installing Metrics Server with Helm");
        await _metricsServerInstaller.AddRepositoryAsync(cancellationToken).ConfigureAwait(false);
        await _metricsServerInstaller.InstallAsync(cancellationToken).ConfigureAwait(false);
        Console.WriteLine("✔ Installed Metrics Server with Helm");
        break;
      case KSailDistributionType.K3d:
        Console.WriteLine("✔ K3d Metrics Server is enabled");
        break;
      default:
        throw new NotSupportedException($"the '{config.Spec.Project.Distribution}' distribution is not supported.");
    }
  }

  void HandleNoMetricsServer()
  {
    switch (config.Spec.Project.Distribution)
    {
      case KSailDistributionType.Kind:
        Console.WriteLine("✔ Kind does not install Metrics Server by default");
        break;
      case KSailDistributionType.K3d:
        Console.WriteLine("✔ K3d Metrics Server is disabled");
        break;
      default:
        throw new NotSupportedException($"the '{config.Spec.Project.Distribution}' distribution is not supported.");
    }
  }
}
