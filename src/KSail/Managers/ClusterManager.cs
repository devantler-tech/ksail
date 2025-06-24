using DevantlerTech.ContainerEngineProvisioner.Core;
using DevantlerTech.KubernetesProvisioner.Cluster.Core;
using KSail;
using KSail.Commands.Validate.Handlers;
using KSail.Factories;
using KSail.Models;
using KSail.Models.Project.Enums;

namespace KSail.Managers;

class ClusterManager(KSailCluster config) : IBootstrapManager, ICleanupManager
{
  readonly IKubernetesClusterProvisioner _clusterProvisioner = ClusterProvisionerFactory.Create(config);
  readonly ContainerEngineManager _containerEngineManager = new(config);
  readonly KSailValidateCommandHandler _ksailValidateCommandHandler = new(config, "./");
  public async Task BootstrapAsync(CancellationToken cancellationToken = default)
  {
    await CheckPrerequisites(cancellationToken).ConfigureAwait(false);

    if (!await Validate(config, cancellationToken).ConfigureAwait(false))
      throw new KSailException("validation failed");
    await ProvisionCluster(cancellationToken).ConfigureAwait(false);
  }

  public async Task CleanupAsync(CancellationToken cancellationToken = default) => await _clusterProvisioner.DeleteAsync(config.Metadata.Name, cancellationToken).ConfigureAwait(false);

  async Task CheckPrerequisites(CancellationToken cancellationToken)
  {
    Console.WriteLine($"📋 Checking prerequisites");
    await _containerEngineManager.CheckContainerEngineIsRunning(cancellationToken).ConfigureAwait(false);
    Console.WriteLine("► checking if cluster exists");
    if (await _clusterProvisioner.ExistsAsync(config.Metadata.Name, cancellationToken).ConfigureAwait(false))
    {
      throw new KSailException(
        $"cluster '{config.Metadata.Name}' is already running."
        + Environment.NewLine
        + "  - if you want to recreate the cluster, use 'ksail down' before running 'ksail up' again."
        + Environment.NewLine
        + "  - if you want to update the cluster, use 'ksail update' instead.");
    }
    Console.WriteLine("✔ cluster does not exist");
    Console.WriteLine();
  }

  async Task<bool> Validate(KSailCluster config, CancellationToken cancellationToken = default)
  {
    if (config.Spec.Validation.ValidateOnUp)
    {
      await _ksailValidateCommandHandler.HandleAsync(cancellationToken).ConfigureAwait(false);
      Console.WriteLine();
    }
    return true;
  }

  async Task ProvisionCluster(CancellationToken cancellationToken = default)
  {
    Console.WriteLine($"☸️ Provisioning cluster '{config.Spec.Project.Distribution.ToString().ToLower(System.Globalization.CultureInfo.CurrentCulture)}-{config.Metadata.Name}'");
    await _clusterProvisioner.CreateAsync(config.Metadata.Name, config.Spec.Project.DistributionConfigPath, cancellationToken).ConfigureAwait(false);
    Console.WriteLine();
  }
}
