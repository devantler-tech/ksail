using DevantlerTech.KubernetesGenerator.Native;
using k8s.Models;

namespace KSail.Commands.Gen.Handlers.Native;

class KSailGenNativeWorkloadsJobCommandHandler(string outputFile, bool overwrite) : ICommandHandler
{
  readonly JobGenerator _generator = new();
  public async Task HandleAsync(CancellationToken cancellationToken = default)
  {
    var model = new V1Job
    {
      ApiVersion = "batch/v1",
      Kind = "Job",
      Metadata = new V1ObjectMeta
      {
        Name = "my-job"
      },
      Spec = new V1JobSpec
      {
        Template = new V1PodTemplateSpec
        {
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
            ],
            RestartPolicy = "OnFailure"
          }
        }
      }
    };
    await _generator.GenerateAsync(model, outputFile, overwrite, cancellationToken: cancellationToken).ConfigureAwait(false);
  }
}
