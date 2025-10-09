using System.CommandLine;
using DevantlerTech.KubernetesGenerator.Native;
using DevantlerTech.KubernetesGenerator.Native.Models;
using Docker.DotNet.Models;
using k8s.Models;

namespace KSail.Commands.Gen.Handlers.Native;

class KSailGenNativeSecretCommandHandler(string outputFile, bool overwrite) : ICommandHandler
{
  readonly GenericSecretGenerator _generator = new();
  public async Task HandleAsync(CancellationToken cancellationToken = default)
  {
    var model = new GenericSecret("my-secret")
    {
    };
    await _generator.GenerateAsync(model, outputFile, overwrite, cancellationToken: cancellationToken).ConfigureAwait(false);
  }
}
