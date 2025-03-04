using Devantler.KubernetesGenerator.Native;
using k8s.Models;

namespace KSail.Commands.Gen.Handlers.Native;

class KSailGenNativeRoleCommandHandler
{
  readonly RoleGenerator _generator = new();

  internal async Task<int> HandleAsync(string outputFile, CancellationToken cancellationToken = default)
  {
    var model = new V1Role()
    {
      ApiVersion = "rbac.authorization.k8s.io/v1",
      Kind = "Role",
      Metadata = new V1ObjectMeta()
      {
        Name = "my-role",
        NamespaceProperty = "<namespace>",
      },
      Rules = [
        new V1PolicyRule()
        {
          ApiGroups = [""],
          Resources = [""],
          Verbs = [""]
        }
      ]
    };
    await _generator.GenerateAsync(model, outputFile, cancellationToken: cancellationToken).ConfigureAwait(false);
    return 0;
  }
}
