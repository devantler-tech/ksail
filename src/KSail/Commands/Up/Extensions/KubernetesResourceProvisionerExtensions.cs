using System.Threading.Tasks;
using Devantler.KubernetesProvisioner.Resources.Native;
using k8s;
using k8s.Models;

namespace KSail.Commands.Up.Extensions;

static class KubernetesResourceProvisionerExtensions
{
  public static async Task CreateNamespaceAsync(this KubernetesResourceProvisioner provisioner, string namespaceName, CancellationToken cancellationToken = default)
  {
    var namespaceList = await provisioner.ListNamespaceAsync(cancellationToken: cancellationToken).ConfigureAwait(false);
    bool namespaceExists = namespaceList.Items.Any(x => x.Metadata.Name == namespaceName);
    if (namespaceExists)
    {
      Console.WriteLine($"✓ '{namespaceName}' namespace already exists");
      return;
    }
    _ = await provisioner.CreateNamespaceAsync(new V1Namespace
    {
      Metadata = new V1ObjectMeta
      {
        Name = namespaceName
      }
    }, cancellationToken: cancellationToken).ConfigureAwait(false);
    Console.WriteLine($"✔ '{namespaceName}' namespace created");
  }
}
