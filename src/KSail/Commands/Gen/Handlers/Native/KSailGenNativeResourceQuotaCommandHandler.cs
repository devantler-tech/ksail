using System.CommandLine;
using DevantlerTech.KubernetesGenerator.Native;
using DevantlerTech.KubernetesGenerator.Native.Models;
using k8s.Models;

namespace KSail.Commands.Gen.Handlers.Native;

class KSailGenNativeResourceQuotaCommandHandler(string outputFile, bool overwrite) : ICommandHandler
{
  readonly ResourceQuotaGenerator _generator = new();

  public async Task HandleAsync(CancellationToken cancellationToken = default)
  {
    var model = new ResourceQuota("my-resource-quota")
    {
    };
    await _generator.GenerateAsync(model, outputFile, overwrite, cancellationToken: cancellationToken).ConfigureAwait(false);
  }
}
