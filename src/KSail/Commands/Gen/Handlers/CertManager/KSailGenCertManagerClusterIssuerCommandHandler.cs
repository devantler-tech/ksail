using Devantler.KubernetesGenerator.CertManager;
using Devantler.KubernetesGenerator.CertManager.Models;
using k8s.Models;

namespace KSail.Commands.Gen.Handlers.CertManager;

class KSailGenCertManagerClusterIssuerCommandHandler(string outputFile, bool overwrite) : ICommandHandler
{
  readonly CertManagerClusterIssuerGenerator _generator = new();

  public async Task<int> HandleAsync(CancellationToken cancellationToken = default)
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
    return 0;
  }
}
