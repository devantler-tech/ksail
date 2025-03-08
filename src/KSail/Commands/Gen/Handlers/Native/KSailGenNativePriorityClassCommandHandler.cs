using Devantler.KubernetesGenerator.Native;
using k8s.Models;

namespace KSail.Commands.Gen.Handlers.Native;

class KSailGenNativePriorityClassCommandHandler(string outputFile, bool overwrite)
{
  readonly PriorityClassGenerator _generator = new();
  internal async Task<int> HandleAsync(CancellationToken cancellationToken = default)
  {
    var model = new V1PriorityClass()
    {
      ApiVersion = "scheduling.k8s.io/v1",
      Kind = "PriorityClass",
      Metadata = new V1ObjectMeta()
      {
        Name = "my-priority-class"
      },
      Value = 1000,
      GlobalDefault = false,
      Description = "<description>",
    };
    await _generator.GenerateAsync(model, outputFile, overwrite, cancellationToken: cancellationToken).ConfigureAwait(false);
    return 0;
  }
}
