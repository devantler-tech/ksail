using DevantlerTech.KubernetesGenerator.Native;
using k8s.Models;

namespace KSail.Commands.Gen.Handlers.Native;

class KSailGenNativePersistentVolumeCommandHandler(string outputFile, bool overwrite) : ICommandHandler
{
  readonly PersistentVolumeGenerator _generator = new();

  public async Task HandleAsync(CancellationToken cancellationToken = default)
  {
    var model = new V1PersistentVolume()
    {
      ApiVersion = "v1",
      Kind = "PersistentVolume",
      Metadata = new V1ObjectMeta()
      {
        Name = "my-persistent-volume"
      },
      Spec = new V1PersistentVolumeSpec()
      {
        Capacity = new Dictionary<string, ResourceQuantity>()
        {
          ["storage"] = new ResourceQuantity("5Gi")
        },
        AccessModes = ["ReadWriteOnce"],
        StorageClassName = "my-storage-class",
      }
    };
    await _generator.GenerateAsync(model, outputFile, overwrite, cancellationToken: cancellationToken).ConfigureAwait(false);
    return 0;
  }
}
