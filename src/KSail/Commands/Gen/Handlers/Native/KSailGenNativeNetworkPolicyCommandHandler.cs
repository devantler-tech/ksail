using System.CommandLine;
using DevantlerTech.KubernetesGenerator.Native;
using k8s.Models;

namespace KSail.Commands.Gen.Handlers.Native;

class KSailGenNativeNetworkPolicyCommandHandler(string outputFile, bool overwrite) : ICommandHandler
{
  readonly NetworkPolicyGenerator _generator = new();

  public async Task HandleAsync(CancellationToken cancellationToken = default)
  {
    var model = new V1NetworkPolicy()
    {
      ApiVersion = "networking.k8s.io/v1",
      Kind = "NetworkPolicy",
      Metadata = new V1ObjectMeta()
      {
        Name = "my-network-policy",
        NamespaceProperty = "my-namespace"
      },
      Spec = new V1NetworkPolicySpec()
      {
        PodSelector = new V1LabelSelector()
        {
          MatchLabels = new Dictionary<string, string>()
        },
        PolicyTypes =
        [
          "Ingress",
          "Egress",
        ],
        Ingress = [
          new V1NetworkPolicyIngressRule()
          {
             FromProperty = []
          }
        ],
        Egress = [
          new V1NetworkPolicyEgressRule()
          {
            To = []
          }
        ]
      }
    };
    await _generator.GenerateAsync(model, outputFile, overwrite, cancellationToken: cancellationToken).ConfigureAwait(false);
  }
}


