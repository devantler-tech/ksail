
using Devantler.ContainerEngineProvisioner.Core;
using Devantler.ContainerEngineProvisioner.Docker;
using KSail;
using KSail.Factories;
using KSail.Models;
using KSail.Models.MirrorRegistry;
using KSail.Models.Project.Enums;

class MirrorRegistryBootstrapper(KSailCluster config) : IBootstrapper
{
  readonly DockerProvisioner _containerEngineProvisioner = ContainerEngineProvisionerFactory.Create(config);
  public async Task BootstrapAsync(CancellationToken cancellationToken = default)
  {
    if (config.Spec.Project.MirrorRegistries)
    {
      await CreateMirrorRegistries(config, cancellationToken).ConfigureAwait(false);
      await BootstrapMirrorRegistries(config, cancellationToken).ConfigureAwait(false);
    }
  }

  async Task CreateMirrorRegistries(KSailCluster config, CancellationToken cancellationToken)
  {
    Console.WriteLine("🧮 Creating mirror registries");

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

  async Task BootstrapMirrorRegistries(KSailCluster config, CancellationToken cancellationToken)
  {
    switch ((config.Spec.Project.Provider, config.Spec.Project.Distribution))
    {
      case (KSailProviderType.Docker, KSailDistributionType.Native):
        Console.WriteLine("🔼 Bootstrapping mirror registries");
        string[] args = [
        "get",
            "nodes",
            "--name", $"{config.Metadata.Name}"
        ];
        var (_, output) = await Devantler.KindCLI.Kind.RunAsync(args, silent: true, cancellationToken: cancellationToken).ConfigureAwait(false);
        if (output.Contains("No kind nodes found for cluster", StringComparison.OrdinalIgnoreCase))
        {
          throw new KSailException(output);
        }
        string[] nodes = output.Split(Environment.NewLine, StringSplitOptions.RemoveEmptyEntries);
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
          Console.WriteLine($"► connect '{mirrorRegistry.Name}' to 'kind-{config.Metadata.Name}' network");
          var dockerClient = _containerEngineProvisioner.Client;
          var dockerNetworks = await dockerClient.Networks.ListNetworksAsync(cancellationToken: cancellationToken).ConfigureAwait(false);
          var kindNetworks = dockerNetworks.Where(x => x.Name.Contains("kind", StringComparison.OrdinalIgnoreCase));
          foreach (var kindNetwork in kindNetworks)
          {
            string containerId = await _containerEngineProvisioner.GetContainerIdAsync(mirrorRegistry.Name, cancellationToken).ConfigureAwait(false);
            await _containerEngineProvisioner.ConnectContainerToNetworkByNameAsync(mirrorRegistry.Name, kindNetwork.Name, cancellationToken).ConfigureAwait(false);
          }
        }
        Console.WriteLine($"✔ mirror registries connected to 'kind' networks");
        Console.WriteLine();
        break;
      case (KSailProviderType.Docker, KSailDistributionType.K3s):
        break;
      default:
        break;
    }
  }

  async Task AddMirrorRegistryToContainerd(string containerName, KSailMirrorRegistry mirrorRegistry, CancellationToken cancellationToken)
  {
    // https://github.com/containerd/containerd/blob/main/docs/hosts.md
    var proxy = mirrorRegistry.Proxy;
    string mirrorRegistryHost = proxy.Url.Host;
    if (mirrorRegistryHost.Contains("docker.io", StringComparison.OrdinalIgnoreCase))
    {
      mirrorRegistryHost = "docker.io";
    }
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
