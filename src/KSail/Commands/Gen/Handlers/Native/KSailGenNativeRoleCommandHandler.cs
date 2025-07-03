using System.CommandLine;
using DevantlerTech.KubernetesGenerator.Native;
using k8s.Models;

namespace KSail.Commands.Gen.Handlers.Native;

class KSailGenNativeRoleCommandHandler(string outputFile, bool overwrite) : ICommandHandler
{
  readonly RoleGenerator _generator = new();

  public async Task HandleAsync(CancellationToken cancellationToken = default)
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
    await _generator.GenerateAsync(model, outputFile, overwrite, cancellationToken: cancellationToken).ConfigureAwait(false);
  }
}
