using DevantlerTech.KubernetesGenerator.Native;
using k8s.Models;

namespace KSail.Commands.Gen.Handlers.Native;

class KSailGenNativePriorityClassCommandHandler(string outputFile, bool overwrite) : ICommandHandler
{
  readonly PriorityClassGenerator _generator = new();
  public async Task HandleAsync(CancellationToken cancellationToken = default)
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
  }
}
