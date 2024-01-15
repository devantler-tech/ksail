using System.Text;
using k8s;
using k8s.Models;

namespace KSail.Provisioners;

sealed class KubernetesProvisioner : IProvisioner, IDisposable
{
  readonly Kubernetes kubernetesClient = new(KubernetesClientConfiguration.BuildDefaultConfig());

  /// <inheritdoc/>
  internal async Task CreateNamespaceAsync(string name)
  {
    console.WriteLine($"🌐 Creating '{name}' namespace...");
    var fluxSystemNamespace = new V1Namespace
    {
      ApiVersion = "v1",
      Kind = "Namespace",
      Metadata = new V1ObjectMeta
      {
        Name = name
      }
    };
    _ = await kubernetesClient.CreateNamespaceAsync(fluxSystemNamespace);
    console.WriteLine("✔ Namespace created...");
    console.WriteLine();
  }

  internal async Task CreateSecretAsync(string name, Dictionary<string, string> data, string @namespace = "default")
  {
    console.WriteLine($"► Deploying '{name}' secret to '{@namespace}' namespace");
    var sopsGpgSecret = new V1Secret
    {
      ApiVersion = "v1",
      Kind = "Secret",
      Metadata = new V1ObjectMeta
      {
        Name = "sops-age",
        NamespaceProperty = "flux-system"
      },
      Type = "Opaque",
      Data = data.ToDictionary(
        pair => pair.Key,
        pair => Encoding.UTF8.GetBytes(pair.Value)
      )
    };
    _ = await kubernetesClient.CreateNamespacedSecretAsync(sopsGpgSecret, "flux-system");
  }

  public void Dispose()
  {
    kubernetesClient.Dispose();
    GC.SuppressFinalize(this);
  }
}
