using DevantlerTech.KubernetesGenerator.Native;
using k8s.Models;

namespace KSail.Commands.Gen.Handlers.Native;

class KSailGenNativeAccountCommandHandler(string outputFile, bool overwrite) : ICommandHandler
{
  readonly ServiceAccountGenerator _generator = new();

  public async Task<int> HandleAsync(CancellationToken cancellationToken = default)
  {
    var model = new V1ServiceAccount()
    {
      ApiVersion = "v1",
      Kind = "ServiceAccount",
      Metadata = new V1ObjectMeta()
      {
        Name = "my-service-account",
      },
    };
    await _generator.GenerateAsync(model, outputFile, overwrite, cancellationToken: cancellationToken).ConfigureAwait(false);
    return 0;
  }
}
