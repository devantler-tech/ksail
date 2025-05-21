using Devantler.ContainerEngineProvisioner.Core;
using Devantler.KubernetesProvisioner.Cluster.Core;
using KSail;
using KSail.Commands.Validate.Handlers;
using KSail.Factories;
using KSail.Models;
using KSail.Models.Project.Enums;

namespace KSail.Managers;

class ContainerEngineManager(KSailCluster config) : IManager
{
  readonly IContainerEngineProvisioner _containerEngineProvisioner = ContainerEngineProvisionerFactory.Create(config);

  public async Task CheckContainerEngineIsRunning(CancellationToken cancellationToken = default)
  {
    Console.WriteLine($"► checking '{config.Spec.Project.ContainerEngine}' is running");
    for (int i = 0; i < 5; i++)
    {
      Console.WriteLine($"► pinging '{config.Spec.Project.ContainerEngine}' (try {i + 1})");
      if (await _containerEngineProvisioner.CheckReadyAsync(cancellationToken).ConfigureAwait(false))
      {
        Console.WriteLine($"✔ {config.Spec.Project.ContainerEngine} is running");
        return;
      }
      await Task.Delay(1000, cancellationToken).ConfigureAwait(false);
    }
    throw new KSailException($"{config.Spec.Project.ContainerEngine} is not running after multiple attempts.");
  }
}
