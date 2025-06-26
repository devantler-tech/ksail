using DevantlerTech.KubernetesGenerator.Native;
using k8s.Models;

namespace KSail.Commands.Gen.Handlers.Native;

class KSailGenNativePodDisruptionBudgetCommandHandler(string outputFile, bool overwrite) : ICommandHandler
{
  readonly PodDisruptionBudgetGenerator _generator = new();
  public async Task HandleAsync(CancellationToken cancellationToken = default)
  {
    var model = new V1PodDisruptionBudget()
    {
      ApiVersion = "policy/v1",
      Kind = "PodDisruptionBudget",
      Metadata = new V1ObjectMeta()
      {
        Name = "my-pod-disruption-budget"
      },
      Spec = new V1PodDisruptionBudgetSpec()
      {
        Selector = new V1LabelSelector()
        {
          MatchLabels = new Dictionary<string, string>()
        },
        MinAvailable = new IntstrIntOrString("2"),
        MaxUnavailable = new IntstrIntOrString("1"),
      }
    };
    await _generator.GenerateAsync(model, outputFile, overwrite, cancellationToken: cancellationToken).ConfigureAwait(false);
  }
}
