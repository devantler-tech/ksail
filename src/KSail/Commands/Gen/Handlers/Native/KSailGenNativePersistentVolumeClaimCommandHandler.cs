using Devantler.KubernetesGenerator.Native;
using k8s.Models;

namespace KSail.Commands.Gen.Handlers.Native;

class KSailGenNativePersistentVolumeClaimCommandHandler
{
  readonly PersistentVolumeClaimGenerator _generator = new();
  internal async Task<int> HandleAsync(string outputFile, CancellationToken cancellationToken = default)
  {
    var model = new V1PersistentVolumeClaim
    {
      ApiVersion = "v1",
      Kind = "PersistentVolumeClaim",
      Metadata = new V1ObjectMeta()
      {
        Name = "my-persistent-volume-claim",
        NamespaceProperty = "my-namespace"
      },
      Spec = new V1PersistentVolumeClaimSpec()
      {
        AccessModes = [
          "ReadWriteOnce"
        ],
        Resources = new V1VolumeResourceRequirements()
        {
          Requests = new Dictionary<string, ResourceQuantity>()
          {
            { "storage", new ResourceQuantity("1Gi") }
          }
        },
        StorageClassName = "",
      }
    };
    await _generator.GenerateAsync(model, outputFile, cancellationToken: cancellationToken).ConfigureAwait(false);
    return 0;
  }
}
