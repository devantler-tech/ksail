using System.Text;
using Devantler.ContainerEngineProvisioner.Docker;
using Devantler.KubernetesProvisioner.Cluster.Core;
using Devantler.KubernetesProvisioner.CNI.Cilium;
using Devantler.KubernetesProvisioner.Deployment.Core;
using Devantler.KubernetesProvisioner.GitOps.Core;
using Devantler.KubernetesProvisioner.Resources.Native;
using Devantler.SecretManager.SOPS.LocalAge;
using k8s;
using k8s.Models;
using KSail.Commands.Validate.Handlers;
using KSail.Factories;
using KSail.Models;
using KSail.Models.MirrorRegistry;
using KSail.Models.Project.Enums;
using KSail.Utils;

namespace KSail.Commands.Up.Handlers;

class KSailUpCommandHandler(KSailCluster config)
{
  readonly SOPSLocalAgeSecretManager _secretManager = new();
  readonly DockerProvisioner _containerEngineProvisioner = ContainerEngineProvisionerFactory.Create(config);
  readonly IDeploymentToolProvisioner _deploymentTool = DeploymentToolProvisionerFactory.Create(config);
  readonly IKubernetesClusterProvisioner _clusterProvisioner = ClusterProvisionerFactory.Create(config);
  readonly CiliumProvisioner? _cniProvisioner = CNIProvisionerFactory.Create(config);
  readonly KSailValidateCommandHandler _ksailValidateCommandHandler = new(config);

  internal async Task<int> HandleAsync(CancellationToken cancellationToken = default)
  {
    await CheckPrerequisites(cancellationToken).ConfigureAwait(false);

    if (!await Validate(config, cancellationToken).ConfigureAwait(false))
    {
      return 1;
    }

    await ProvisionCluster(cancellationToken).ConfigureAwait(false);
    await BootstrapSource(config, cancellationToken).ConfigureAwait(false);
    await BootstrapMirrorRegistries(config, cancellationToken).ConfigureAwait(false);
    await BootstrapCNI(config, cancellationToken).ConfigureAwait(false);
    BootstrapIngressController(config);
    BootstrapGatewayController(config);
    await BootstrapSecretManager(config, cancellationToken).ConfigureAwait(false);
    await BootstrapDeploymentTool(config, cancellationToken).ConfigureAwait(false);

    await ReconcileAsync(cancellationToken).ConfigureAwait(false);
    return 0;
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
    if (config.Spec.Project.Distribution == KSailDistributionType.K3s)
    {
      Console.WriteLine();
    }
  }

  async Task BootstrapSource(KSailCluster config, CancellationToken cancellationToken)
  {
    await CreateOCISourceRegistry(config, cancellationToken).ConfigureAwait(false);
    switch ((config.Spec.Project.Provider, config.Spec.Project.Distribution, config.Spec.Project.DeploymentTool))
    {
      case (KSailProviderType.Docker, KSailDistributionType.Native, KSailDeploymentToolType.Flux):
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
    Console.WriteLine();
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
    }
  }

  async Task BootstrapMirrorRegistries(KSailCluster config, CancellationToken cancellationToken)
  {
    await CreateMirrorRegistries(config, cancellationToken).ConfigureAwait(false);
    if (config.Spec.Project.MirrorRegistries)
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

  async Task BootstrapCNI(KSailCluster config, CancellationToken cancellationToken)
  {
    Console.WriteLine("🌐 Bootstrapping CNI");
    if (config.Spec.Project.CNI == KSailCNIType.Default)
    {
      switch (config.Spec.Project.Provider, config.Spec.Project.Distribution)
      {
        case (KSailProviderType.Docker, KSailDistributionType.Native):
          Console.WriteLine("► Kind deploys the kindnetd CNI by default");
          break;
        case (KSailProviderType.Docker, KSailDistributionType.K3s):
          Console.WriteLine("► K3d deploys the Flannel CNI by default");
          break;
        default:
          break;
      }
    }

    if (_cniProvisioner != null)
    {
      Console.WriteLine($"► installing '{config.Spec.Project.CNI}' CNI");
      await _cniProvisioner.InstallAsync(cancellationToken).ConfigureAwait(false);
    }

    Console.WriteLine($"✔ '{config.Spec.Project.CNI}' CNI installed");
    Console.WriteLine();
  }

  static void BootstrapIngressController(KSailCluster config)
  {
    Console.WriteLine("🚦 Bootstrapping Ingress Controller");
    if (config.Spec.Project.IngressController == KSailIngressControllerType.Default)
    {
      switch (config.Spec.Project.Provider, config.Spec.Project.Distribution)
      {
        case (KSailProviderType.Docker, KSailDistributionType.Native):
          Console.WriteLine("► Kind does not deploy an Ingress Controller by default");
          break;
        case (KSailProviderType.Docker, KSailDistributionType.K3s):
          Console.WriteLine("► K3d deploys the Traefik Ingress Controller by default");
          break;
        default:
          break;
      }
    }
    Console.WriteLine("✔ Ingress Controller bootstrapped");
    Console.WriteLine();
  }

  static void BootstrapGatewayController(KSailCluster config)
  {
    Console.WriteLine("🚦🆕 Bootstrapping Gateway Controller");
    if (config.Spec.Project.GatewayController == KSailGatewayControllerType.Default)
    {
      switch (config.Spec.Project.Provider, config.Spec.Project.Distribution)
      {
        case (KSailProviderType.Docker, KSailDistributionType.Native):
          Console.WriteLine("► Kind does not deploy a Gateway Controller by default");
          break;
        case (KSailProviderType.Docker, KSailDistributionType.K3s):
          Console.WriteLine("► K3d does not deploy a Gateway Controller by default");
          break;
        default:
          break;
      }
    }
    Console.WriteLine("✔ Gateway Controller bootstrapped");
    Console.WriteLine();
  }

  async Task BootstrapSecretManager(KSailCluster config, CancellationToken cancellationToken)
  {
    using var resourceProvisioner = new KubernetesResourceProvisioner(config.Spec.Connection.Kubeconfig, config.Spec.Connection.Context);
    if (config.Spec.Project.SecretManager)
    {
      Console.WriteLine("🔐 Bootstrapping SOPS secret manager");
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

  async Task BootstrapDeploymentTool(KSailCluster config, CancellationToken cancellationToken = default)
  {
    Console.WriteLine($"🚀 Bootstrapping {config.Spec.Project.DeploymentTool}");
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

  async Task ReconcileAsync(CancellationToken cancellationToken)
  {
    if (config.Spec.Validation.ReconcileOnUp)
    {
      Console.WriteLine();
      Console.WriteLine("🔄 Reconciling changes");
      string kubernetesDirectory = config.Spec.Project.KustomizationPath.TrimStart('.', '/').Split('/').First();
      await _deploymentTool.ReconcileAsync(kubernetesDirectory, config.Spec.Connection.Timeout, cancellationToken).ConfigureAwait(false);
      Console.WriteLine("✔ reconciliation completed");
      Console.WriteLine();
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
