using Devantler.KubernetesGenerator.Native;
using k8s.Models;

namespace KSail.Commands.Gen.Handlers.Native;

class KSailGenNativeWorkloadsDaemonSetCommandHandler(string outputFile, bool overwrite) : ICommandHandler
{
  readonly DaemonSetGenerator _generator = new();
  public async Task<int> HandleAsync(CancellationToken cancellationToken = default)
  {
    var model = new V1DaemonSet
    {
      ApiVersion = "apps/v1",
      Kind = "DaemonSet",
      Metadata = new V1ObjectMeta
      {
        Name = "my-daemon-set"
      },
      Spec = new V1DaemonSetSpec
      {
        Selector = new V1LabelSelector
        {
          MatchLabels = new Dictionary<string, string>
          {
            ["app"] = "my-daemon-set"
          }
        },
        Template = new V1PodTemplateSpec
        {
          Metadata = new V1ObjectMeta
          {
            Labels = new Dictionary<string, string>
            {
              ["app"] = "my-daemon-set"
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
    return 0;
  }
}
