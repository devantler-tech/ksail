using Devantler.KubernetesGenerator.Native;
using k8s.Models;

namespace KSail.Commands.Gen.Handlers.Native;

class KSailGenNativeServiceCommandHandler(string outputFile, bool overwrite) : ICommandHandler
{
  readonly ServiceGenerator _generator = new();
  public async Task<int> HandleAsync(CancellationToken cancellationToken = default)
  {
    var model = new V1Service
    {
      ApiVersion = "networking.k8s.io/v1",
      Kind = "Service",
      Metadata = new V1ObjectMeta()
      {
        Name = "my-service",
      },
      Spec = new V1ServiceSpec()
      {
        Ports =
        [
          new V1ServicePort()
          {
            Name = "my-port",
            Port = 0,
            TargetPort = 0,
          },
        ],
        Selector = new Dictionary<string, string>()
      }
    };
    await _generator.GenerateAsync(model, outputFile, overwrite, cancellationToken: cancellationToken).ConfigureAwait(false);
    return 0;
  }
}
