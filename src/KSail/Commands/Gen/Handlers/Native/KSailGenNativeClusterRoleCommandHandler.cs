using DevantlerTech.KubernetesGenerator.Native;
using k8s.Models;

namespace KSail.Commands.Gen.Handlers.Native;

class KSailGenNativeClusterRoleCommandHandler(string outputFile, bool overwrite) : ICommandHandler
{
  readonly ClusterRoleGenerator _generator = new();
  public async Task HandleAsync(CancellationToken cancellationToken = default)
  {
    var model = new V1ClusterRole()
    {
      ApiVersion = "rbac.authorization.k8s.io/v1",
      Kind = "ClusterRole",
      Metadata = new V1ObjectMeta()
      {
        Name = "my-cluster-role"
      },
      Rules =
      [
        new V1PolicyRule()
        {
          ApiGroups = [""],
          Resources = ["secrets"],
          Verbs = ["get", "watch", "list"]
        }
      ]
    };
    await _generator.GenerateAsync(model, outputFile, overwrite, cancellationToken: cancellationToken).ConfigureAwait(false);
  }
}
