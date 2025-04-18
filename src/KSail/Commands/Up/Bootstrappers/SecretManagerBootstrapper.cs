using System.Text;
using Devantler.Keys.Age;
using Devantler.KubernetesProvisioner.Resources.Native;
using Devantler.SecretManager.Core;
using Devantler.SecretManager.SOPS.LocalAge;
using k8s;
using k8s.Models;
using KSail;
using KSail.Commands.Up.Extensions;
using KSail.Models;
using KSail.Models.Project.Enums;
using KSail.Utils;

class SecretManagerBootstrapper(KSailCluster config) : IBootstrapper, IDisposable
{
  readonly KubernetesResourceProvisioner _kubernetesResourceProvisioner = new(config.Spec.Connection.Kubeconfig, config.Spec.Connection.Context);
  readonly SOPSLocalAgeSecretManager _secretManager = new();
  public async Task BootstrapAsync(CancellationToken cancellationToken = default)
  {
    if (config.Spec.Project.SecretManager)
    {
      Console.WriteLine("🔐 Bootstrapping SOPS secret manager");
      switch (config.Spec.Project.DeploymentTool)
      {
        case KSailDeploymentToolType.Kubectl:
          BootstrapSOPSForKubectl();
          break;
        case KSailDeploymentToolType.Flux:
          await BootstrapSOPSForFluxAsync(cancellationToken).ConfigureAwait(false);
          break;
        default:
          throw new KSailException($"the '{config.Spec.Project.DeploymentTool}' Deployment Tool is not supported.");
      }
    }
    Console.WriteLine();
  }

  static void BootstrapSOPSForKubectl()
  {
    Console.WriteLine($"► the kubectl deployment tool uses 'ksops' to manage SOPS encrypted secrets");
    Console.WriteLine($"  - 'ksops' is currently not managed by KSail. If you want to use it, please install and configure it manually.");
  }

  async Task BootstrapSOPSForFluxAsync(CancellationToken cancellationToken = default)
  {
    Console.WriteLine($"► creating 'flux-system' namespace");
    await _kubernetesResourceProvisioner.CreateNamespaceAsync("flux-system", cancellationToken).ConfigureAwait(false);
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

    _ = await _kubernetesResourceProvisioner.CreateNamespacedSecretAsync(secret, secret.Metadata.NamespaceProperty, cancellationToken: cancellationToken).ConfigureAwait(false);
    Console.WriteLine("✔ 'sops-age' secret created");
  }

  public void Dispose()
  {
    _kubernetesResourceProvisioner?.Dispose();
    GC.SuppressFinalize(this);
  }
}
