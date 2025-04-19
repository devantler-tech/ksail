
using Devantler.ContainerEngineProvisioner.Core;
using Devantler.ContainerEngineProvisioner.Docker;
using KSail;
using KSail.Factories;
using KSail.Models;
using KSail.Models.Project.Enums;

class GitOpsSourceBootstrapper(KSailCluster config) : IBootstrapper
{
  readonly DockerProvisioner _containerEngineProvisioner = ContainerEngineProvisionerFactory.Create(config);

  public async Task BootstrapAsync(CancellationToken cancellationToken = default)
  {
    if (config.Spec.Project.DeploymentTool == KSailDeploymentToolType.Flux)
    {
      switch (config.Spec.Project.Provider)
      {
        case KSailProviderType.Docker or KSailProviderType.Podman:
          Console.WriteLine("📦 Bootstrapping GitOps source...");
          await CreateOCISourceRegistry(config, cancellationToken).ConfigureAwait(false);
          await BootstrapOCISource(cancellationToken).ConfigureAwait(false);
          Console.WriteLine();
          break;
        default:
          throw new KSailException($"unsupported provider '{config.Spec.Project.Provider}'.");
      }
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
      case (KSailDistributionType.Native, KSailDeploymentToolType.Flux):
        Console.WriteLine($"► connect OCI source registry to 'kind-{config.Metadata.Name}' network");
        var dockerClient = _containerEngineProvisioner.Client;
        var dockerNetworks = await dockerClient.Networks.ListNetworksAsync(cancellationToken: cancellationToken).ConfigureAwait(false);
        var kindNetworks = dockerNetworks.Where(x => x.Name.Contains("kind", StringComparison.OrdinalIgnoreCase));
        foreach (var kindNetwork in kindNetworks)
        {
          string containerName = config.Spec.DeploymentTool.Flux.Source.Url.Segments.Last();
          if (kindNetwork.Containers.Values.Any(x => x.Name == containerName))
          {
            continue;
          }
          string containerId = await _containerEngineProvisioner.GetContainerIdAsync(containerName, cancellationToken).ConfigureAwait(false);
          await _containerEngineProvisioner.ConnectContainerToNetworkByNameAsync(containerName, kindNetwork.Name, cancellationToken).ConfigureAwait(false);
        }
        Console.WriteLine("✔ OCI source registry connected to 'kind' networks");
        break;
      default:
        break;
    }
  }
}
