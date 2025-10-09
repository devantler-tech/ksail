using System.CommandLine;
using DevantlerTech.KubernetesGenerator.Native;
using DevantlerTech.KubernetesGenerator.Native.Models;
using k8s.Models;

namespace KSail.Commands.Gen.Handlers.Native;

class KSailGenNativeRoleBindingCommandHandler(string outputFile, bool overwrite) : ICommandHandler
{
  readonly RoleBindingGenerator _generator = new();

  public async Task HandleAsync(CancellationToken cancellationToken = default)
  {
    var model = new RoleBinding("my-role-binding")
    {
      RoleRef = new RoleBindingRoleRef
      {
        Name = "my-role",
        Kind = RoleBindingRoleRefKind.Role,
      },
      Subjects = []
    };
    await _generator.GenerateAsync(model, outputFile, overwrite, cancellationToken: cancellationToken).ConfigureAwait(false);
  }
}
