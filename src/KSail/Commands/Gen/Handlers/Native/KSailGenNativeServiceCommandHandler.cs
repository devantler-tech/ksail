using System.CommandLine;
using DevantlerTech.KubernetesGenerator.Native;
using k8s.Models;

namespace KSail.Commands.Gen.Handlers.Native;

class KSailGenNativeServiceCommandHandler(string outputFile, bool overwrite) : ICommandHandler
{
  readonly NodePortServiceGenerator _generator = new();
  public async Task HandleAsync(CancellationToken cancellationToken = default)
  {
    var model = new V1Service
    {
      ApiVersion = "v1",
      Kind = "Service",
      Metadata = new V1ObjectMeta
      {
        Name = "my-service",
        Labels = new Dictionary<string, string>
        {
          ["app"] = "my-service"
        }
      },
      Spec = new V1ServiceSpec
      {
        Type = "NodePort",
        Selector = new Dictionary<string, string>
        {
          ["app"] = "my-service"
        },
        Ports =
        [
          new V1ServicePort
          {
            Name = "http",
            Port = 80,
            TargetPort = 80,
            Protocol = "TCP",
            NodePort = 30080
          }
        ]
      }
    };
    await _generator.GenerateAsync(model, outputFile, overwrite, cancellationToken: cancellationToken).ConfigureAwait(false);
  }
}
