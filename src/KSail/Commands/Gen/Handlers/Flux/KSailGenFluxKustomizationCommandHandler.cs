using DevantlerTech.KubernetesGenerator.Flux;
using DevantlerTech.KubernetesGenerator.Flux.Models;
using DevantlerTech.KubernetesGenerator.Flux.Models.Kustomization;

namespace KSail.Commands.Gen.Handlers.Flux;

class KSailGenFluxKustomizationCommandHandler(string outputFile, bool overwrite) : ICommandHandler
{
  readonly FluxKustomizationGenerator _generator = new();
  public async Task HandleAsync(CancellationToken cancellationToken = default)
  {
    var fluxKustomization = new FluxKustomization
    {
      Metadata = new FluxNamespacedMetadata
      {
        Name = "flux-kustomization"
      },
      Spec = new FluxKustomizationSpec
      {
        Interval = "60m",
        Timeout = "3m",
        RetryInterval = "2m",
        SourceRef = new FluxKustomizationSpecSourceRef
        {
          Kind = FluxKustomizationSpecSourceRefKind.OCIRepository,
          Name = "flux-system"
        },
        Path = "path/to/kustomize-kustomization-dir",
        Prune = true,
        Wait = true
      }
    };

    await _generator.GenerateAsync(fluxKustomization, outputFile, overwrite, cancellationToken: cancellationToken).ConfigureAwait(false);
  }
}
