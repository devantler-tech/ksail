using DevantlerTech.KubernetesGenerator.Native;
using k8s.Models;

namespace KSail.Commands.Gen.Handlers.Native;

class KSailGenNativeWorkloadsStatefulSetCommandHandler(string outputFile, bool overwrite) : ICommandHandler
{
  readonly StatefulSetGenerator _generator = new();
  public async Task HandleAsync(CancellationToken cancellationToken = default)
  {
    var model = new V1StatefulSet
    {
      ApiVersion = "apps/v1",
      Kind = "StatefulSet",
      Metadata = new V1ObjectMeta
      {
        Name = "my-stateful-set"
      },
      Spec = new V1StatefulSetSpec
      {
        Selector = new V1LabelSelector
        {
          MatchLabels = new Dictionary<string, string>
          {
            ["app"] = "my-stateful-set"
          }
        },
        ServiceName = "my-service",
        Replicas = 1,
        Template = new V1PodTemplateSpec
        {
          Metadata = new V1ObjectMeta
          {
            Labels = new Dictionary<string, string>
            {
              ["app"] = "my-stateful-set"
            }
          },
          Spec = new V1PodSpec
          {
            Containers =
            [
              new V1Container
              {
                Name = "my-container",
                Image = "my-image",
                ImagePullPolicy = "IfNotPresent",
                Command = []
              }
            ]
          }
        }
      }
    };
    await _generator.GenerateAsync(model, outputFile, overwrite, cancellationToken: cancellationToken).ConfigureAwait(false);
  }
}
