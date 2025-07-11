using DevantlerTech.KubernetesGenerator.CertManager;
using DevantlerTech.KubernetesGenerator.CertManager.Models;
using k8s.Models;

namespace KSail.Commands.Gen.Handlers.CertManager;

class KSailGenCertManagerClusterIssuerCommandHandler(string outputFile, bool overwrite) : ICommandHandler
{
  readonly CertManagerClusterIssuerGenerator _generator = new();

  public async Task HandleAsync(CancellationToken cancellationToken = default)
  {
    var clusterIssuer = new CertManagerClusterIssuer
    {
      Metadata = new V1ObjectMeta
      {
        Name = "selfsigned",
        NamespaceProperty = "cert-manager"
      },
      Spec = new CertManagerClusterIssuerSpec
      {
        SelfSigned = new object()

      }
    };
    await _generator.GenerateAsync(clusterIssuer, outputFile, overwrite, cancellationToken: cancellationToken).ConfigureAwait(false);
  }
}
