using Devantler.KubernetesGenerator.Kustomize;
using Devantler.KubernetesGenerator.Kustomize.Models;

namespace KSail.Commands.Gen.Handlers.Kustomize;

class KSailGenKustomizeComponentCommandHandler(string outputFile, bool overwrite) : ICommandHandler
{
  readonly KustomizeComponentGenerator _generator = new();
  public async Task<int> HandleAsync(CancellationToken cancellationToken = default)
  {
    var kustomizeComponent = new KustomizeComponent()
    {
      Resources = [],
      Patches = [],
      ConfigMapGenerator = [],
      SecretGenerator = []
    };

    await _generator.GenerateAsync(kustomizeComponent, outputFile, overwrite, cancellationToken: cancellationToken).ConfigureAwait(false);
    return 0;
  }
}
