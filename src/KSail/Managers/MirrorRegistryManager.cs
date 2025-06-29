using DevantlerTech.ContainerEngineProvisioner.Core;
using DevantlerTech.ContainerEngineProvisioner.Docker;
using DevantlerTech.ContainerEngineProvisioner.Podman;
using DevantlerTech.KubernetesProvisioner.Cluster.Core;
using Docker.DotNet;
using KSail;
using KSail.Factories;
using KSail.Models;
using KSail.Models.MirrorRegistry;
using KSail.Models.Project.Enums;

namespace KSail.Managers;

class MirrorRegistryManager(KSailCluster config) : IBootstrapManager, ICleanupManager
{
  readonly IContainerEngineProvisioner _containerEngineProvisioner = ContainerEngineProvisionerFactory.Create(config);
  readonly IKubernetesClusterProvisioner _kubernetesDistributionProvisioner = ClusterProvisionerFactory.Create(config);
  public async Task BootstrapAsync(CancellationToken cancellationToken = default)
  {
    if (config.Spec.Project.MirrorRegistries)
    {
      await CreateMirrorRegistries(config, cancellationToken).ConfigureAwait(false);
      await BootstrapMirrorRegistries(config, cancellationToken).ConfigureAwait(false);
    }
  }

  public async Task CleanupAsync(CancellationToken cancellationToken = default)
  {
    var clusters = await _kubernetesDistributionProvisioner.ListAsync(cancellationToken).ConfigureAwait(false);
    if (!clusters.Any())
    {
      await DeleteRegistriesAsync(cancellationToken).ConfigureAwait(false);
    }
  }

  async Task CreateMirrorRegistries(KSailCluster config, CancellationToken cancellationToken)
  {
    Console.WriteLine("🧮 Creating mirror registries");

    await _containerEngineProvisioner.PullImageAsync("registry:3", cancellationToken).ConfigureAwait(false);
    var tasks = config.Spec.MirrorRegistries.Select(async mirrorRegistry =>
    {
      Console.WriteLine($"► creating mirror registry '{mirrorRegistry.Name}' for '{mirrorRegistry.Proxy.Url}'");
      await _containerEngineProvisioner.CreateRegistryAsync(
        mirrorRegistry.Name,
        mirrorRegistry.HostPort,
        mirrorRegistry.Proxy.Url,
        cancellationToken).ConfigureAwait(false);
    });
    await Task.WhenAll(tasks).ConfigureAwait(false);
    Console.WriteLine("✔ mirror registries created");
    Console.WriteLine();
  }

  async Task DeleteRegistriesAsync(CancellationToken cancellationToken = default)
  {
    string containerName = config.Spec.LocalRegistry.Name;
    bool ksailRegistryExists = await _containerEngineProvisioner.CheckContainerExistsAsync(containerName, cancellationToken).ConfigureAwait(false);
    if (ksailRegistryExists)
    {
      Console.WriteLine("► Deleting local registry");
      await _containerEngineProvisioner.DeleteRegistryAsync(containerName, cancellationToken).ConfigureAwait(false);
      Console.WriteLine($"✓ '{containerName}' deleted.");
    }

    Console.WriteLine("► Deleting mirror registries");
    if (config.Spec.Project.MirrorRegistries)
    {
      var deleteTasks = config.Spec.MirrorRegistries.Select(async mirrorRegistry =>
      {
        bool mirrorRegistryExists = await _containerEngineProvisioner.CheckContainerExistsAsync(mirrorRegistry.Name, cancellationToken).ConfigureAwait(false);
        if (mirrorRegistryExists)
        {
          await _containerEngineProvisioner.DeleteRegistryAsync(mirrorRegistry.Name, cancellationToken).ConfigureAwait(false);
          Console.WriteLine($"✓ '{mirrorRegistry.Name}' deleted.");
        }
      });
      await Task.WhenAll(deleteTasks).ConfigureAwait(false);
    }
  }

  async Task BootstrapMirrorRegistries(KSailCluster config, CancellationToken cancellationToken)
  {
    switch (config.Spec.Project.Distribution)
    {
      case KSailDistributionType.Kind:
        Console.WriteLine("🔼 Bootstrapping mirror registries");
        string[] args = [
        "get",
          "nodes",
          "--name", $"{config.Metadata.Name}"
          ];
        var (_, output) = await DevantlerTech.KindCLI.Kind.RunAsync(args, silent: true, cancellationToken: cancellationToken).ConfigureAwait(false);
        if (output.Contains("No kind nodes found for cluster", StringComparison.OrdinalIgnoreCase))
          throw new KSailException(output);
        string[] nodes = output.Split(Environment.NewLine, StringSplitOptions.RemoveEmptyEntries);
        // TODO: Remove this workaround when Kind CLI no longer outputs the experimental podman provider message
        nodes = [.. nodes.Where(line => !line.Contains("enabling experimental podman provider", StringComparison.OrdinalIgnoreCase))];
        foreach (string node in nodes)
        {
          foreach (var mirrorRegistry in config.Spec.MirrorRegistries)
          {
            string containerName = node;
            Console.WriteLine($"► adding '{mirrorRegistry.Name}' as containerd mirror registry to '{node}'");
            await AddMirrorRegistryToContainerd(containerName, mirrorRegistry, cancellationToken).ConfigureAwait(false);
          }
          Console.WriteLine($"✔ '{node}' containerd mirror registries bootstrapped.");
        }
        foreach (var mirrorRegistry in config.Spec.MirrorRegistries)
        {
          var dockerClient = _containerEngineProvisioner switch
          {
            DockerProvisioner dockerProvisioner => dockerProvisioner.Client,
            PodmanProvisioner podmanProvisioner => podmanProvisioner.Client,
            _ => throw new NotSupportedException("Unsupported container engine provisioner")
          };
          var dockerNetworks = await dockerClient.Networks.ListNetworksAsync(cancellationToken: cancellationToken).ConfigureAwait(false);
          var kindNetworks = dockerNetworks.Where(x => x.Name.Contains("kind", StringComparison.OrdinalIgnoreCase));
          foreach (var kindNetwork in kindNetworks)
          {
            Console.WriteLine($"► connect '{mirrorRegistry.Name}' to '{kindNetwork.Name}' network");
            string containerId = await _containerEngineProvisioner.GetContainerIdAsync(mirrorRegistry.Name, cancellationToken).ConfigureAwait(false);
            try
            {
              await _containerEngineProvisioner.ConnectContainerToNetworkByNameAsync(mirrorRegistry.Name, kindNetwork.Name, cancellationToken).ConfigureAwait(false);
            }
            catch (DockerApiException ex) when (ex.Message.Contains("already exists in network", StringComparison.OrdinalIgnoreCase))
            {
              Console.WriteLine($"✔ '{mirrorRegistry.Name}' is already connected to '{kindNetwork.Name}' network");
            }
          }
        }
        Console.WriteLine($"✔ mirror registries connected to 'kind' networks");
        Console.WriteLine();
        break;
      case KSailDistributionType.K3d:
        break;
      default:
        throw new NotSupportedException($"Container Engine '{config.Spec.Project.ContainerEngine}' with distribution '{config.Spec.Project.Distribution}' is not supported.");
    }
  }

  async Task AddMirrorRegistryToContainerd(string containerName, KSailMirrorRegistry mirrorRegistry, CancellationToken cancellationToken)
  {
    // https://github.com/containerd/containerd/blob/main/docs/hosts.md
    var proxy = mirrorRegistry.Proxy;
    string mirrorRegistryHost = proxy.Url.Host;
    if (mirrorRegistryHost.Contains("docker.io", StringComparison.OrdinalIgnoreCase))
      mirrorRegistryHost = "docker.io";
    string registryDir = $"/etc/containerd/certs.d/{mirrorRegistryHost}";
    await _containerEngineProvisioner.CreateDirectoryInContainerAsync(containerName, registryDir, true, cancellationToken).ConfigureAwait(false);
    string host = $"{mirrorRegistry.Name}:5000";
    string hostsToml = $"""
    server = "{proxy.Url}"

    [host."http://{host}"]
      capabilities = ["pull", "resolve"]
      skip_verify = true
    """;
    await _containerEngineProvisioner.CreateFileInContainerAsync(containerName, $"{registryDir}/hosts.toml", hostsToml, cancellationToken).ConfigureAwait(false);
  }
}
