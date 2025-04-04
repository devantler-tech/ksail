using KSail.Commands.Init.Generators.SubGenerators;
using KSail.Models;

namespace KSail.Commands.Init.Generators;

class ProjectGenerator
{
  readonly TemplateKustomizeGenerator _templateKustomizeGenerator = new();
  internal async Task GenerateAsync(KSailCluster config, CancellationToken cancellationToken = default) =>
    await _templateKustomizeGenerator.GenerateAsync(config, cancellationToken).ConfigureAwait(false);
}
