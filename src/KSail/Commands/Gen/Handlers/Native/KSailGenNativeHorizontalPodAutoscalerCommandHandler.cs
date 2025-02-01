using Devantler.KubernetesGenerator.Native;
using k8s.Models;

namespace KSail.Commands.Gen.Handlers.Native;

class KSailGenNativeHorizontalPodAutoscalerCommandHandler
{
  readonly HorizontalPodAutoscalerGenerator _generator = new();
  internal async Task<int> HandleAsync(string outputFile, CancellationToken cancellationToken = default)
  {
    var model = new V2HorizontalPodAutoscaler()
    {
      ApiVersion = "autoscaling/v2",
      Kind = "HorizontalPodAutoscaler",
      Metadata = new V1ObjectMeta()
      {
        Name = "my-hpa"
      },
      Spec = new V2HorizontalPodAutoscalerSpec()
      {
        MinReplicas = 2,
        MaxReplicas = 4,
        ScaleTargetRef = new V2CrossVersionObjectReference()
        {
          ApiVersion = "apps/v1",
          Kind = "Deployment",
          Name = "my-deployment"
        },
        Behavior = new V2HorizontalPodAutoscalerBehavior(),
        Metrics = []
      }
    };
    await _generator.GenerateAsync(model, outputFile, cancellationToken: cancellationToken).ConfigureAwait(false);
    return 0;
  }
}
