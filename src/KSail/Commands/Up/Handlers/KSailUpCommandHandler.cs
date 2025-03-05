using System.Text;
using Devantler.ContainerEngineProvisioner.Docker;
using Devantler.KubernetesProvisioner.Cluster.Core;
using Devantler.KubernetesProvisioner.Cluster.K3d;
using Devantler.KubernetesProvisioner.Cluster.Kind;
using Devantler.KubernetesProvisioner.GitOps.Flux;
using Devantler.KubernetesProvisioner.Resources.Native;
using Devantler.SecretManager.SOPS.LocalAge;
using Docker.DotNet.Models;
using k8s;
using k8s.Models;
using KSail.Commands.Lint.Handlers;
using KSail.Models;
using KSail.Models.MirrorRegistry;
using KSail.Models.Project.Enums;
using KSail.Utils;

namespace KSail.Commands.Up.Handlers;

class KSailUpCommandHandler
{
  //TODO: readonly CiliumProvisioner _cniProvisioner = new();
  readonly SOPSLocalAgeSecretManager _secretManager = new();
  readonly DockerProvisioner _engineProvisioner;
  readonly FluxProvisioner _deploymentTool;
  readonly IKubernetesClusterProvisioner _clusterProvisioner;
  readonly IKubernetesCNIProvisioner _cniProvisioner;
  readonly KSailCluster _config;
  readonly KSailLintCommandHandler _ksailLintCommandHandler;

  internal KSailUpCommandHandler(KSailCluster config)
  {
    _ksailLintCommandHandler = new KSailLintCommandHandler(config);
    _engineProvisioner = config.Spec.Project.Engine switch
    {
      KSailEngineType.Docker => new DockerProvisioner(),
      _ => throw new NotSupportedException($"The container engine '{config.Spec.Project.Engine}' is not supported.")
    };
    _clusterProvisioner = (config.Spec.Project.Engine, config.Spec.Project.Distribution) switch
    {
      (KSailEngineType.Docker, KSailKubernetesDistributionType.Native) => new KindProvisioner(),
      (KSailEngineType.Docker, KSailKubernetesDistributionType.K3s) => new K3dProvisioner(),
      _ => throw new NotSupportedException($"The distribution '{config.Spec.Project.Distribution}' is not supported.")
    };
    _deploymentTool = config.Spec.Project.DeploymentTool switch
    {
      KSailDeploymentToolType.Flux => new FluxProvisioner(config.Spec.Connection.Context),
      _ => throw new NotSupportedException($"The Deployment tool '{config.Spec.Project.DeploymentTool}' is not supported.")
    };
    _config = config;
  }

  internal async Task<int> HandleAsync(CancellationToken cancellationToken = default)
  {
    if (!await Lint(_config, cancellationToken).ConfigureAwait(false))
    {
      return 1;
    }

    if (!await CheckEngineIsRunning(cancellationToken).ConfigureAwait(false))
    {
      return 1;
    }

    await CreateOCISourceRegistry(_config, cancellationToken).ConfigureAwait(false);
    await CreateMirrorRegistries(_config, cancellationToken).ConfigureAwait(false);

    await ProvisionCluster(cancellationToken).ConfigureAwait(false);

    await BootstrapOCISourceRegistry(_config, cancellationToken).ConfigureAwait(false);
    await BootstrapMirrorRegistries(_config, cancellationToken).ConfigureAwait(false);
    await BootstrapCNI(_config, cancellationToken).ConfigureAwait(false);
    await BootstrapSecretManager(_config, cancellationToken).ConfigureAwait(false);
    await BootstrapDeploymentTool(_config, cancellationToken).ConfigureAwait(false);

    if (_config.Spec.Validation.ReconcileOnUp)
    {
      Console.WriteLine("🔄 Reconciling kustomizations");
      await _deploymentTool.ReconcileAsync(_config.Spec.Connection.Timeout, cancellationToken).ConfigureAwait(false);
      Console.WriteLine();
    }
    return 0;
  }

  async Task<bool> CheckEngineIsRunning(CancellationToken cancellationToken = default)
  {
    string engineEmoji = _config.Spec.Project.Engine switch
    {
      KSailEngineType.Docker => "🐳",
      _ => throw new KSailException($"The container engine '{_config.Spec.Project.Engine}' is not supported.")
    };
    Console.WriteLine($"{engineEmoji} Checking {_config.Spec.Project.Engine} is running");

    for (int i = 0; i < 5; i++)
    {
      Console.WriteLine($"► pinging {_config.Spec.Project.Engine} engine (try {i + 1})");
      if (await _engineProvisioner.CheckReadyAsync(cancellationToken).ConfigureAwait(false))
      {
        Console.WriteLine($"✔ {_config.Spec.Project.Engine} is running");
        Console.WriteLine();
        return true;
      }
      await Task.Delay(1000, cancellationToken).ConfigureAwait(false);
    }
    Console.WriteLine();
    throw new KSailException($"{_config.Spec.Project.Engine} is not running after multiple attempts.");
  }

  async Task CreateOCISourceRegistry(KSailCluster config, CancellationToken cancellationToken = default)
  {
    if (config.Spec.Project.Engine == KSailEngineType.Docker && config.Spec.Project.DeploymentTool == KSailDeploymentToolType.Flux)
    {
      Console.WriteLine("📥 Create OCI source registry");
      Console.WriteLine($"► creating '{config.Spec.DeploymentTool.Flux.Source.Url}' as OCI source registry");
      await _engineProvisioner
       .CreateRegistryAsync(
        config.Spec.LocalRegistry.Name,
        config.Spec.LocalRegistry.HostPort,
        cancellationToken: cancellationToken
      ).ConfigureAwait(false);
      Console.WriteLine("✔ OCI source registry created");
      Console.WriteLine();
    }
  }

  async Task CreateMirrorRegistries(KSailCluster config, CancellationToken cancellationToken)
  {
    if (config.Spec.Project.MirrorRegistries)
    {
      Console.WriteLine("🧮 Creating mirror registries");
      var tasks = config.Spec.MirrorRegistries.Select(async mirrorRegistry =>
      {
        Console.WriteLine($"► creating mirror registry '{mirrorRegistry.Name}' for '{mirrorRegistry.Proxy?.Url}'");
        await _engineProvisioner
         .CreateRegistryAsync(mirrorRegistry.Name, mirrorRegistry.HostPort, mirrorRegistry.Proxy?.Url, cancellationToken).ConfigureAwait(false);
      });

      await Task.WhenAll(tasks).ConfigureAwait(false);
      Console.WriteLine("✔ mirror registries created");
      Console.WriteLine();
    }
  }

  async Task<bool> Lint(KSailCluster config, CancellationToken cancellationToken = default)
  {
    if (config.Spec.Validation.LintOnUp)
    {
      Console.WriteLine("🔍 Linting manifests");
      bool success = await _ksailLintCommandHandler.HandleAsync(cancellationToken).ConfigureAwait(false);
      Console.WriteLine();
      return success;
    }
    return true;
  }

  async Task ProvisionCluster(CancellationToken cancellationToken = default)
  {
    Console.WriteLine($"🚀 Provisioning cluster '{_config.Spec.Project.Distribution.ToString().ToLower(System.Globalization.CultureInfo.CurrentCulture)}-{_config.Metadata.Name}'");
    await _clusterProvisioner.CreateAsync(_config.Metadata.Name, _config.Spec.Project.DistributionConfigPath, cancellationToken).ConfigureAwait(false);
    Console.WriteLine();
  }

  async Task BootstrapOCISourceRegistry(KSailCluster config, CancellationToken cancellationToken)
  {
    switch ((config.Spec.Project.Engine, config.Spec.Project.Distribution))
    {
      case (KSailEngineType.Docker, KSailKubernetesDistributionType.Native):
        Console.WriteLine("🔼 Botstrapping OCI source registry");
        Console.WriteLine($"► connect OCI source registry to 'kind-{config.Metadata.Name}' network");
        var dockerClient = _engineProvisioner.Client;
        var dockerNetworks = await dockerClient.Networks.ListNetworksAsync(cancellationToken: cancellationToken).ConfigureAwait(false);
        var kindNetworks = dockerNetworks.Where(x => x.Name.Contains("kind", StringComparison.OrdinalIgnoreCase));
        foreach (var kindNetwork in kindNetworks)
        {
          string containerName = config.Spec.DeploymentTool.Flux.Source.Url.Segments.Last();
          if (kindNetwork.Containers.Values.Any(x => x.Name == containerName))
          {
            continue;
          }
          string containerId = await _engineProvisioner.GetContainerIdAsync(containerName, cancellationToken).ConfigureAwait(false);
          await dockerClient.Networks.ConnectNetworkAsync(kindNetwork.ID, new NetworkConnectParameters
          {
            Container = containerId
          }, cancellationToken).ConfigureAwait(false);
        }
        Console.WriteLine("✔ OCI source registry connected to 'kind' networks");
        break;
      default:
        break;
    }
    Console.WriteLine();
  }

  async Task BootstrapMirrorRegistries(KSailCluster config, CancellationToken cancellationToken)
  {
    if (config.Spec.Project.MirrorRegistries)
    {
      switch ((config.Spec.Project.Engine, config.Spec.Project.Distribution))
      {
        case (KSailEngineType.Docker, KSailKubernetesDistributionType.Native):
          Console.WriteLine("🔼 Bootstrapping mirror registries");
          string[] args = [
          "get",
            "nodes",
            "--name", $"{_config.Metadata.Name}"
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
            var dockerClient = _engineProvisioner.Client;
            var dockerNetworks = await dockerClient.Networks.ListNetworksAsync(cancellationToken: cancellationToken).ConfigureAwait(false);
            var kindNetworks = dockerNetworks.Where(x => x.Name.Contains("kind", StringComparison.OrdinalIgnoreCase));
            foreach (var kindNetwork in kindNetworks)
            {
              string containerId = await _engineProvisioner.GetContainerIdAsync(mirrorRegistry.Name, cancellationToken).ConfigureAwait(false);
              await dockerClient.Networks.ConnectNetworkAsync(kindNetwork.ID, new NetworkConnectParameters
              {
                Container = containerId
              }, cancellationToken).ConfigureAwait(false);
            }
          }
          Console.WriteLine($"✔ mirror registries connected to 'kind' networks");
          Console.WriteLine();
          break;
        case (KSailEngineType.Docker, KSailKubernetesDistributionType.K3s):
          break;
        default:
          break;
      }
    }

    async Task AddMirrorRegistryToContainerd(string containerName, KSailMirrorRegistry mirrorRegistry, CancellationToken cancellationToken)
    {
      // https://github.com/containerd/containerd/blob/main/docs/hosts.md
      string registryDir = $"/etc/containerd/certs.d/{mirrorRegistry.Name}";
      await _engineProvisioner.CreateDirectoryInContainerAsync(containerName, registryDir, true, cancellationToken).ConfigureAwait(false);
      string host = $"{mirrorRegistry.Name}:5000";
      string hostsToml = $"""
      server = "{mirrorRegistry.Proxy.Url}"

      [host."http://{host}"]
        capabilities = ["pull", "resolve"]
        skip_verify = true
      """;
      await _engineProvisioner.CreateFileInContainerAsync(containerName, $"{registryDir}/hosts.toml", hostsToml, cancellationToken).ConfigureAwait(false);
    }
  }

  async Task BootstrapDeploymentTool(KSailCluster config, CancellationToken cancellationToken = default)
  {
    Console.WriteLine($"🔼 Bootstrapping {config.Spec.Project.DeploymentTool}");
    using var resourceProvisioner = new KubernetesResourceProvisioner(config.Spec.Connection.Context);
    Console.WriteLine($"► creating 'flux-system' namespace (--context={config.Spec.Connection.Context})");
    await CreateFluxSystemNamespace(resourceProvisioner, cancellationToken).ConfigureAwait(false);

    string scheme = config.Spec.DeploymentTool.Flux.Source.Url.Scheme;
    string host = "localhost";
    string absolutePath = config.Spec.DeploymentTool.Flux.Source.Url.AbsolutePath;
    var sourceUrlFromHost = new Uri($"{scheme}://{host}:{config.Spec.LocalRegistry.HostPort}{absolutePath}");
    string kubernetesDirectory = config.Spec.Project.KustomizationPath.TrimStart('.', '/').Split('/').First();
    await _deploymentTool.PushManifestsAsync(sourceUrlFromHost, kubernetesDirectory, cancellationToken: cancellationToken).ConfigureAwait(false);
    string ociKustomizationPath = config.Spec.Project.KustomizationPath[kubernetesDirectory.Length..].TrimStart('/');
    await _deploymentTool.BootstrapAsync(
      config.Spec.DeploymentTool.Flux.Source.Url,
      ociKustomizationPath,
      true,
      cancellationToken
    ).ConfigureAwait(false);
    Console.WriteLine();
  }

  async Task BootstrapCNI(KSailCluster config, CancellationToken cancellationToken)
  {
    switch (config.Spec.Project.CNI)
    {
      case KSailCNIType.Cilium:
        Console.WriteLine("🐝 Installing Cilium CNI");
        await _cniProvisioner.InstallAsync(config.Spec.Connection.Context, cancellationToken).ConfigureAwait(false);
        Console.WriteLine("✔ Cilium CNI installed");
        Console.WriteLine();
        break;
      case KSailCNIType.Default:
      default:
        break;
    }
  }
  async Task BootstrapSecretManager(KSailCluster config, CancellationToken cancellationToken)
  {
    using var resourceProvisioner = new KubernetesResourceProvisioner(config.Spec.Connection.Context);
    if (config.Spec.Project.SecretManager == KSailSecretManagerType.SOPS)
    {
      Console.WriteLine("🔼 Bootstrapping SOPS secret manager");
      Console.WriteLine($"► creating 'flux-system' namespace (--context={config.Spec.Connection.Context})");
      await CreateFluxSystemNamespace(resourceProvisioner, cancellationToken).ConfigureAwait(false);

      var sopsConfig = await SopsConfigLoader.LoadAsync(cancellationToken: cancellationToken).ConfigureAwait(false);
      string publicKey = sopsConfig.CreationRules.First(x => x.PathRegex.Contains(config.Metadata.Name, StringComparison.OrdinalIgnoreCase)).Age.Split(',')[0].Trim();

      Console.WriteLine("► getting private key from SOPS_AGE_KEY_FILE or default location");
      var ageKey = await _secretManager.GetKeyAsync(publicKey, cancellationToken).ConfigureAwait(false);

      Console.WriteLine("► creating 'sops-age' secret in 'flux-system' namespace");
      var secret = new V1Secret
      {
        Metadata = new V1ObjectMeta
        {
          Name = "sops-age",
          NamespaceProperty = "flux-system"
        },
        Type = "Generic",
        Data = new Dictionary<string, byte[]>
        {
          { "age.agekey", Encoding.UTF8.GetBytes(ageKey.PrivateKey) }
        }
      };

      _ = await resourceProvisioner.CreateNamespacedSecretAsync(secret, secret.Metadata.NamespaceProperty, cancellationToken: cancellationToken).ConfigureAwait(false);
      Console.WriteLine("✔ 'sops-age' secret created");
      Console.WriteLine();
    }
  }

  // TODO: Move to generic method on KubernetesResourceProvisioner
  static async Task CreateFluxSystemNamespace(KubernetesResourceProvisioner resourceProvisioner, CancellationToken cancellationToken)
  {
    var namespaceList = await resourceProvisioner.ListNamespaceAsync(cancellationToken: cancellationToken).ConfigureAwait(false);
    bool namespaceExists = namespaceList.Items.Any(x => x.Metadata.Name == "flux-system");
    if (namespaceExists)
    {
      Console.WriteLine("✓ 'flux-system' namespace already exists");
    }
    else
    {
      _ = await resourceProvisioner.CreateNamespaceAsync(new V1Namespace
      {
        Metadata = new V1ObjectMeta
        {
          Name = "flux-system"
        }
      }, cancellationToken: cancellationToken).ConfigureAwait(false);
      Console.WriteLine("✔ 'flux-system' namespace created");
    }
  }
}
