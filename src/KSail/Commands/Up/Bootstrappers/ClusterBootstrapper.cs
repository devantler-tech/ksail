using Devantler.ContainerEngineProvisioner.Docker;
using Devantler.KubernetesProvisioner.Cluster.Core;
using KSail;
using KSail.Commands.Validate.Handlers;
using KSail.Factories;
using KSail.Models;
using KSail.Models.Project.Enums;

class ClusterBootstrapper(KSailCluster config) : IBootstrapper
{
  readonly IKubernetesClusterProvisioner _clusterProvisioner = ClusterProvisionerFactory.Create(config);
  readonly DockerProvisioner _containerEngineProvisioner = ContainerEngineProvisionerFactory.Create(config);
  readonly KSailValidateCommandHandler _ksailValidateCommandHandler = new(config);
  public async Task BootstrapAsync(CancellationToken cancellationToken = default)
  {
    await CheckPrerequisites(cancellationToken).ConfigureAwait(false);

    if (!await Validate(config, cancellationToken).ConfigureAwait(false))
    {
      throw new KSailException("validation failed");
    }
    await ProvisionCluster(cancellationToken).ConfigureAwait(false);
  }

  async Task CheckPrerequisites(CancellationToken cancellationToken)
  {
    Console.WriteLine($"📋 Checking prerequisites");
    await CheckProviderIsRunning(cancellationToken).ConfigureAwait(false);
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

  async Task CheckProviderIsRunning(CancellationToken cancellationToken = default)
  {
    Console.WriteLine($"► checking '{config.Spec.Project.Provider}' is running");
    for (int i = 0; i < 5; i++)
    {
      Console.WriteLine($"► pinging '{config.Spec.Project.Provider}' (try {i + 1})");
      if (await _containerEngineProvisioner.CheckReadyAsync(cancellationToken).ConfigureAwait(false))
      {
        Console.WriteLine($"✔ {config.Spec.Project.Provider} is running");
        return;
      }
      await Task.Delay(1000, cancellationToken).ConfigureAwait(false);
    }
    throw new KSailException($"{config.Spec.Project.Provider} is not running after multiple attempts.");
  }

  async Task<bool> Validate(KSailCluster config, CancellationToken cancellationToken = default)
  {
    if (config.Spec.Validation.ValidateOnUp)
    {
      bool success = await _ksailValidateCommandHandler.HandleAsync("./", cancellationToken).ConfigureAwait(false);
      Console.WriteLine();
      return success;
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
