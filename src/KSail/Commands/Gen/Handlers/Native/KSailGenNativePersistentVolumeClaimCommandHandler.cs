using DevantlerTech.KubernetesGenerator.Native;
using k8s.Models;

namespace KSail.Commands.Gen.Handlers.Native;

class KSailGenNativePersistentVolumeClaimCommandHandler(string outputFile, bool overwrite) : ICommandHandler
{
  readonly PersistentVolumeClaimGenerator _generator = new();
  public async Task HandleAsync(CancellationToken cancellationToken = default)
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
    await _generator.GenerateAsync(model, outputFile, overwrite, cancellationToken: cancellationToken).ConfigureAwait(false);
    return 0;
  }
}
