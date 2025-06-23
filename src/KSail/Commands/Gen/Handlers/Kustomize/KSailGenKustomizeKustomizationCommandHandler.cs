using DevantlerTech.KubernetesGenerator.Kustomize;
using DevantlerTech.KubernetesGenerator.Kustomize.Models;

namespace KSail.Commands.Gen.Handlers.Kustomize;

class KSailGenKustomizeKustomizationCommandHandler(string outputFile, bool overwrite) : ICommandHandler
{
  readonly KustomizeKustomizationGenerator _generator = new();
  public async Task<int> HandleAsync(CancellationToken cancellationToken = default)
  {
    var kustomization = new KustomizeKustomization
    {
      Resources = [],
      Patches = [],
      ConfigMapGenerator = [],
      SecretGenerator = [],
      Components = []
    };
    await _generator.GenerateAsync(kustomization, outputFile, overwrite, cancellationToken: cancellationToken).ConfigureAwait(false);
    return 0;
  }
}

