using DevantlerTech.ContainerEngineProvisioner.Core;
using DevantlerTech.ContainerEngineProvisioner.Docker;
using DevantlerTech.ContainerEngineProvisioner.Podman;
using Docker.DotNet;
using KSail;
using KSail.Factories;
using KSail.Models;
using KSail.Models.Project.Enums;

namespace KSail.Managers;

class GitOpsSourceManager(KSailCluster config) : IBootstrapManager
{
  readonly IContainerEngineProvisioner _containerEngineProvisioner = ContainerEngineProvisionerFactory.Create(config);

  public async Task BootstrapAsync(CancellationToken cancellationToken = default)
  {
    if (config.Spec.Project.DeploymentTool == KSailDeploymentToolType.Flux)
    {
      Console.WriteLine("📦 Bootstrapping GitOps source...");
      await CreateOCISourceRegistry(config, cancellationToken).ConfigureAwait(false);
      await BootstrapOCISource(cancellationToken).ConfigureAwait(false);
      Console.WriteLine();
    }
  }

  async Task CreateOCISourceRegistry(KSailCluster config, CancellationToken cancellationToken = default)
  {
    Console.WriteLine($"► creating '{config.Spec.DeploymentTool.Flux.Source.Url}' as OCI source registry");
    await _containerEngineProvisioner.CreateRegistryAsync(
      config.Spec.LocalRegistry.Name,
      config.Spec.LocalRegistry.HostPort,
      cancellationToken: cancellationToken
    ).ConfigureAwait(false);
    Console.WriteLine("✔ OCI source registry created");
  }

  async Task BootstrapOCISource(CancellationToken cancellationToken)
  {
    switch ((config.Spec.Project.Distribution, config.Spec.Project.DeploymentTool))
    {
      case (KSailDistributionType.Kind, KSailDeploymentToolType.Flux):
        var dockerClient = _containerEngineProvisioner switch
        {
          DockerProvisioner dockerProvisioner => dockerProvisioner.Client,
          PodmanProvisioner podmanProvisioner => podmanProvisioner.Client,
          _ => throw new NotSupportedException($"Unsupported container engine provisioner")
        };
        var dockerNetworks = await dockerClient.Networks.ListNetworksAsync(cancellationToken: cancellationToken).ConfigureAwait(false);
        var kindNetworks = dockerNetworks.Where(x => x.Name.Contains("kind", StringComparison.OrdinalIgnoreCase));
        foreach (var kindNetwork in kindNetworks)
        {
          Console.WriteLine($"► connect OCI source registry to '{kindNetwork.Name}' network");
          string containerName = config.Spec.DeploymentTool.Flux.Source.Url.Segments.Last();
          if (kindNetwork.Containers.Values.Any(x => x.Name == containerName))
            continue;
          string containerId = await _containerEngineProvisioner.GetContainerIdAsync(containerName, cancellationToken).ConfigureAwait(false);
          try
          {
            await _containerEngineProvisioner.ConnectContainerToNetworkByNameAsync(containerName, kindNetwork.Name, cancellationToken).ConfigureAwait(false);
            Console.WriteLine($"✔ OCI source registry connected to '{kindNetwork.Name}' network");
          }
          catch (DockerApiException ex) when (ex.Message.Contains("already exists in network", StringComparison.OrdinalIgnoreCase))
          {
            Console.WriteLine($"✔ OCI source registry is already connected to '{kindNetwork.Name}' network");
          }
        }
        break;
      default:
        break;
    }
  }
}
