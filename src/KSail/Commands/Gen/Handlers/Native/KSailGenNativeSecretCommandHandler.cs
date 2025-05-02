using Devantler.KubernetesGenerator.Native;
using k8s.Models;

namespace KSail.Commands.Gen.Handlers.Native;

class KSailGenNativeSecretCommandHandler(string outputFile, bool overwrite) : ICommandHandler
{
  readonly SecretGenerator _generator = new();
  public async Task<int> HandleAsync(CancellationToken cancellationToken = default)
  {
    var model = new V1Secret
    {
      ApiVersion = "v1",
      Kind = "Secret",
      Metadata = new V1ObjectMeta()
      {
        Name = "my-secret",
        NamespaceProperty = "my-namespace"
      },
      Type = "Opaque",
      StringData = new Dictionary<string, string>()
    };
    await _generator.GenerateAsync(model, outputFile, overwrite, cancellationToken: cancellationToken).ConfigureAwait(false);
    return 0;
  }
}
