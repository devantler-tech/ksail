using k8s;
using k8s.Autorest;

namespace KSail.Extensions;

internal static class KubernetesExtensions
{
  internal static Task<HttpOperationResponse<object>> ListKustomizationsWithHttpMessagesAsync(this Kubernetes kubernetesClient, CancellationToken cancellationToken)
  {
    return kubernetesClient.CustomObjects.ListNamespacedCustomObjectWithHttpMessagesAsync(
      "kustomize.toolkit.fluxcd.io",
      "v1",
      "flux-system",
      "kustomizations",
      watch: true,
      cancellationToken: cancellationToken
    );
  }
}
