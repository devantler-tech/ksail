using DevantlerTech.KubernetesGenerator.Flux;
using DevantlerTech.KubernetesGenerator.Flux.Models;
using DevantlerTech.KubernetesGenerator.Flux.Models.HelmRelease;

namespace KSail.Commands.Gen.Handlers.Flux;

class KSailGenFluxHelmReleaseCommandHandler(string outputFile, bool overwrite) : ICommandHandler
{
  readonly FluxHelmReleaseGenerator _generator = new();
  public async Task<int> HandleAsync(CancellationToken cancellationToken = default)
  {
    var helmRelease = new FluxHelmRelease()
    {
      Metadata = new FluxNamespacedMetadata
      {
        Name = "my-helm-release",
        Namespace = "my-namespace"
      },
      Spec = new FluxHelmReleaseSpec(new FluxHelmReleaseSpecChart
      {
        Spec = new FluxHelmReleaseSpecChartSpec
        {
          Chart = "my-chart",
          SourceRef = new FluxHelmReleaseSpecChartSpecSourceRef
          {
            Kind = FluxHelmReleaseSpecChartSpecSourceRefKind.HelmRepository,
            Name = "my-helm-repo"
          }
        }
      })
      {
        Interval = "10m"
      }
    };
    await _generator.GenerateAsync(helmRelease, outputFile, overwrite, cancellationToken: cancellationToken).ConfigureAwait(false);
    return 0;
  }
}
