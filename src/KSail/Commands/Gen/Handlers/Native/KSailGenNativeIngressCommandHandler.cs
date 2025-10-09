using System.CommandLine;
using DevantlerTech.KubernetesGenerator.Native;
using DevantlerTech.KubernetesGenerator.Native.Models;
using k8s.Models;

namespace KSail.Commands.Gen.Handlers.Native;

class KSailGenNativeIngressCommandHandler(string outputFile, bool overwrite) : ICommandHandler
{
  readonly IngressGenerator _generator = new();
  public async Task HandleAsync(CancellationToken cancellationToken = default)
  {
    var model = new Ingress
    {
      Metadata = new Metadata
      {
        Name = "my-ingress",
        Labels = new Dictionary<string, string>
        {
          ["app"] = "my-ingress"
        }
      },
      Rules =
      [
        new IngressRule
        {
          Host = "example.com",
          Path = string.Empty,
          ServiceName = "my-service",
          ServicePort = "80"
        }
      ]
    };
    await _generator.GenerateAsync(model, outputFile, overwrite, cancellationToken: cancellationToken).ConfigureAwait(false);
  }
}
