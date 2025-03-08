using Devantler.KubernetesGenerator.CertManager;
using Devantler.KubernetesGenerator.CertManager.Models;
using k8s.Models;

namespace KSail.Commands.Gen.Handlers.CertManager;

class KSailGenCertManagerClusterIssuerCommandHandler(string outputFile, bool overwrite)
{
  readonly CertManagerClusterIssuerGenerator _generator = new();

  internal async Task<int> HandleAsync(CancellationToken cancellationToken = default)
  {
    var clusterIssuer = new CertManagerClusterIssuer
    {
      Metadata = new V1ObjectMeta
      {
        Name = "selfsigned-cluster-issuer",
        NamespaceProperty = "cert-manager"
      },
      Spec = new CertManagerClusterIssuerSpec
      {
        SelfSigned = new object()

      }
    };
    await _generator.GenerateAsync(clusterIssuer, outputFile, overwrite, cancellationToken: cancellationToken).ConfigureAwait(false);
    return 0;
  }
}
