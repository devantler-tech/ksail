using Devantler.KubernetesGenerator.Native;
using k8s.Models;

namespace KSail.Commands.Gen.Handlers.Native;

class KSailGenNativeWorkloadsCronJobCommandHandler(string outputFile, bool overwrite) : ICommandHandler
{
  readonly CronJobGenerator _generator = new();
  public async Task<int> HandleAsync(CancellationToken cancellationToken = default)
  {
    var model = new V1CronJob
    {
      ApiVersion = "batch/v1",
      Kind = "CronJob",
      Metadata = new V1ObjectMeta
      {
        Name = "my-cron-job"
      },
      Spec = new V1CronJobSpec
      {
        Schedule = "*/1 * * * *",
        JobTemplate = new V1JobTemplateSpec
        {
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
        }
      }

    };
    await _generator.GenerateAsync(model, outputFile, overwrite, cancellationToken: cancellationToken).ConfigureAwait(false);
    return 0;
  }
}
