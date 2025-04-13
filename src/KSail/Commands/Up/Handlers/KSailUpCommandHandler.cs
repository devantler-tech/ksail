using System.Collections.ObjectModel;
using System.Text;
using Devantler.ContainerEngineProvisioner.Docker;
using Devantler.KubectlCLI;
using Devantler.KubernetesProvisioner.Cluster.Core;
using Devantler.KubernetesProvisioner.Cluster.K3d;
using Devantler.KubernetesProvisioner.Cluster.Kind;
using Devantler.KubernetesProvisioner.CNI.Cilium;
using Devantler.KubernetesProvisioner.Deployment.Core;
using Devantler.KubernetesProvisioner.Deployment.Kubectl;
using Devantler.KubernetesProvisioner.GitOps.Core;
using Devantler.KubernetesProvisioner.GitOps.Flux;
using Devantler.KubernetesProvisioner.Resources.Native;
using Devantler.SecretManager.SOPS.LocalAge;
using Docker.DotNet.Models;
using k8s;
using k8s.Models;
using KSail.Commands.Validate.Handlers;
using KSail.Models;
using KSail.Models.MirrorRegistry;
using KSail.Models.Project.Enums;
using KSail.Utils;

namespace KSail.Commands.Up.Handlers;

class KSailUpCommandHandler
{
  readonly SOPSLocalAgeSecretManager _secretManager = new();
  readonly DockerProvisioner _containerEngineProvisioner;
  readonly IDeploymentToolProvisioner _deploymentTool;
  readonly IKubernetesClusterProvisioner _clusterProvisioner;
  readonly CiliumProvisioner? _cniProvisioner;
  readonly KSailCluster _config;
  readonly KSailValidateCommandHandler _ksailValidateCommandHandler;
  readonly Uri _ociRegistryFromHost;

  internal KSailUpCommandHandler(KSailCluster config)
  {
    _ksailValidateCommandHandler = new KSailValidateCommandHandler(config);
    _containerEngineProvisioner = config.Spec.Project.Provider switch
    {
      KSailProviderType.Docker => new DockerProvisioner(),
      _ => throw new NotSupportedException($"The container engine '{config.Spec.Project.Provider}' is not supported.")
    };
    _clusterProvisioner = (config.Spec.Project.Provider, config.Spec.Project.Distribution) switch
    {
      (KSailProviderType.Docker, KSailDistributionType.Native) => new KindProvisioner(),
      (KSailProviderType.Docker, KSailDistributionType.K3s) => new K3dProvisioner(),
      _ => throw new NotSupportedException($"The distribution '{config.Spec.Project.Distribution}' is not supported.")
    };
    _cniProvisioner = config.Spec.Project.CNI switch
    {
      KSailCNIType.Cilium => new CiliumProvisioner(config.Spec.Connection.Kubeconfig, config.Spec.Connection.Context),
      KSailCNIType.Default => null,
      _ => throw new NotSupportedException($"The CNI '{config.Spec.Project.CNI}' is not supported.")
    };
    string scheme = config.Spec.DeploymentTool.Flux.Source.Url.Scheme;
    string host = "localhost";
    int port = config.Spec.LocalRegistry.HostPort;
    string absolutePath = config.Spec.DeploymentTool.Flux.Source.Url.AbsolutePath;
    _ociRegistryFromHost = new Uri($"{scheme}://{host}:{port}{absolutePath}");
    _deploymentTool = config.Spec.Project.DeploymentTool switch
    {
      KSailDeploymentToolType.Kubectl => new KubectlProvisioner(config.Spec.Connection.Kubeconfig, config.Spec.Connection.Context),
      KSailDeploymentToolType.Flux => new FluxProvisioner(_ociRegistryFromHost, config.Spec.Connection.Kubeconfig, config.Spec.Connection.Context),
      _ => throw new NotSupportedException($"The Deployment tool '{config.Spec.Project.DeploymentTool}' is not supported.")
    };
    _config = config;
  }

  internal async Task<int> HandleAsync(CancellationToken cancellationToken = default)
  {
    await CheckPrerequisites(cancellationToken).ConfigureAwait(false);

    if (!await Validate(_config, cancellationToken).ConfigureAwait(false))
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
      Console.WriteLine("🔄 Reconciling new changes");
      string kubernetesDirectory = _config.Spec.Project.KustomizationPath.TrimStart('.', '/').Split('/').First();
      await _deploymentTool.ReconcileAsync(kubernetesDirectory, _config.Spec.Connection.Timeout, cancellationToken).ConfigureAwait(false);
      Console.WriteLine("✔ reconciliation completed");
      Console.WriteLine();
    }
    return 0;
  }

  async Task CheckPrerequisites(CancellationToken cancellationToken)
  {
    Console.WriteLine($"📋 Checking prerequisites");
    await CheckProviderIsRunning(cancellationToken).ConfigureAwait(false);
    Console.WriteLine("► checking if cluster exists");
    if (await _clusterProvisioner.ExistsAsync(_config.Metadata.Name, cancellationToken).ConfigureAwait(false))
    {
      throw new KSailException(
        $"cluster '{_config.Metadata.Name}' is already running."
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
    Console.WriteLine($"► checking '{_config.Spec.Project.Provider}' is running");
    for (int i = 0; i < 5; i++)
    {
      Console.WriteLine($"► pinging '{_config.Spec.Project.Provider}' (try {i + 1})");
      if (await _containerEngineProvisioner.CheckReadyAsync(cancellationToken).ConfigureAwait(false))
      {
        Console.WriteLine($"✔ {_config.Spec.Project.Provider} is running");
        return;
      }
      await Task.Delay(1000, cancellationToken).ConfigureAwait(false);
    }
    throw new KSailException($"{_config.Spec.Project.Provider} is not running after multiple attempts.");
  }

  async Task CreateOCISourceRegistry(KSailCluster config, CancellationToken cancellationToken = default)
  {
    if (config.Spec.Project.Provider == KSailProviderType.Docker && config.Spec.Project.DeploymentTool == KSailDeploymentToolType.Flux)
    {
      Console.WriteLine("📥 Create OCI source registry");
      Console.WriteLine($"► creating '{config.Spec.DeploymentTool.Flux.Source.Url}' as OCI source registry");
      await _containerEngineProvisioner.CreateRegistryAsync(
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
    Console.WriteLine("🧮 Creating mirror registries");
    if (config.Spec.Project.MirrorRegistries)
    {
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
    }
    Console.WriteLine("✔ mirror registries created");
    Console.WriteLine();
  }

  async Task<bool> Validate(KSailCluster config, CancellationToken cancellationToken = default)
  {
    if (config.Spec.Validation.ValidateOnUp)
    {
      Console.WriteLine("🔍 Validating project files and configuration");
      string kubernetesDirectory = config.Spec.Project.KustomizationPath
        .Replace("./", string.Empty, StringComparison.OrdinalIgnoreCase)
        .Split('/', StringSplitOptions.RemoveEmptyEntries).First();
      bool success = await _ksailValidateCommandHandler.HandleAsync(kubernetesDirectory, cancellationToken).ConfigureAwait(false);
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
    switch ((config.Spec.Project.Provider, config.Spec.Project.Distribution, config.Spec.Project.DeploymentTool))
    {
      case (KSailProviderType.Docker, KSailDistributionType.Native, KSailDeploymentToolType.Flux):
        Console.WriteLine("🔼 Botstrapping OCI source registry");
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
      switch ((config.Spec.Project.Provider, config.Spec.Project.Distribution))
      {
        case (KSailProviderType.Docker, KSailDistributionType.Native):
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
            var dockerClient = _containerEngineProvisioner.Client;
            var dockerNetworks = await dockerClient.Networks.ListNetworksAsync(cancellationToken: cancellationToken).ConfigureAwait(false);
            var kindNetworks = dockerNetworks.Where(x => x.Name.Contains("kind", StringComparison.OrdinalIgnoreCase));
            foreach (var kindNetwork in kindNetworks)
            {
              string containerId = await _containerEngineProvisioner.GetContainerIdAsync(mirrorRegistry.Name, cancellationToken).ConfigureAwait(false);
              await dockerClient.Networks.ConnectNetworkAsync(kindNetwork.ID, new NetworkConnectParameters
              {
                Container = containerId
              }, cancellationToken).ConfigureAwait(false);
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

  async Task BootstrapDeploymentTool(KSailCluster config, CancellationToken cancellationToken = default)
  {
    Console.WriteLine($"🔼 Bootstrapping {config.Spec.Project.DeploymentTool}");
    string kubernetesDirectory = config.Spec.Project.KustomizationPath.TrimStart('.', '/').Split('/').First();
    await _deploymentTool.PushAsync(kubernetesDirectory, cancellationToken: cancellationToken).ConfigureAwait(false);
    if (_deploymentTool is IGitOpsProvisioner gitOpsProvisioner)
    {
      using var resourceProvisioner = new KubernetesResourceProvisioner(config.Spec.Connection.Kubeconfig, config.Spec.Connection.Context);
      Console.WriteLine($"► creating 'flux-system' namespace");
      await CreateFluxSystemNamespace(resourceProvisioner, cancellationToken).ConfigureAwait(false);
      string ociKustomizationPath = config.Spec.Project.KustomizationPath[kubernetesDirectory.Length..].TrimStart('/');
      await gitOpsProvisioner.InstallAsync(
        config.Spec.DeploymentTool.Flux.Source.Url,
        ociKustomizationPath,
        true,
        cancellationToken
      ).ConfigureAwait(false);
      Console.WriteLine();
    }
  }

  async Task BootstrapCNI(KSailCluster config, CancellationToken cancellationToken)
  {
    if (config.Spec.Project.CNI == KSailCNIType.Default || _cniProvisioner == null)
    {
      return;
    }

    Console.WriteLine($"⬡ Installing {config.Spec.Project.CNI} CNI");
    await _cniProvisioner.InstallAsync(cancellationToken).ConfigureAwait(false);
    Console.WriteLine("✔ Cilium CNI installed");
    Console.WriteLine();
  }

  async Task BootstrapSecretManager(KSailCluster config, CancellationToken cancellationToken)
  {
    using var resourceProvisioner = new KubernetesResourceProvisioner(config.Spec.Connection.Kubeconfig, config.Spec.Connection.Context);
    if (config.Spec.Project.SecretManager)
    {
      Console.WriteLine("🔼 Bootstrapping SOPS secret manager");
      Console.WriteLine($"► creating 'flux-system' namespace");
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
