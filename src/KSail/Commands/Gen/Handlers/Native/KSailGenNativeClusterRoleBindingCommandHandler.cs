using Devantler.KubernetesGenerator.Native;
using k8s.Models;

namespace KSail.Commands.Gen.Handlers.Native;

class KSailGenNativeClusterRoleBindingCommandHandler(string outputFile, bool overwrite) : ICommandHandler
{
  readonly ClusterRoleBindingGenerator _generator = new();
  public async Task<int> HandleAsync(CancellationToken cancellationToken = default)
  {
    var model = new V1ClusterRoleBinding()
    {
      ApiVersion = "rbac.authorization.k8s.io/v1",
      Kind = "ClusterRoleBinding",
      Metadata = new V1ObjectMeta()
      {
        Name = "my-cluster-role-binding"
      },
      Subjects =
      [
        new Rbacv1Subject()
        {
          Kind = "Group",
          Name = "my-group",
          ApiGroup = "rbac.authorization.k8s.io"
        }
      ],
      RoleRef = new V1RoleRef()
      {
        Kind = "ClusterRole",
        Name = "my-cluster-role",
        ApiGroup = "rbac.authorization.k8s.io",
      }
    };
    await _generator.GenerateAsync(model, outputFile, overwrite, cancellationToken: cancellationToken).ConfigureAwait(false);
    return 0;
  }
}
