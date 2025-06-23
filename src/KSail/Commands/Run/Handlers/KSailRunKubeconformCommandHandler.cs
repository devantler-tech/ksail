using System.CommandLine;
using DevantlerTech.Keys.Age;
using DevantlerTech.KubeconformCLI;
using DevantlerTech.SecretManager.Core;

namespace KSail.Commands.Run.Handlers;

class KSailRunKubeconformCommandHandler(string[] args) : ICommandHandler
{
  public async Task<int> HandleAsync(CancellationToken cancellationToken)
  {
    _ = await Kubeconform.RunAsync(args, cancellationToken: cancellationToken).ConfigureAwait(false);
    return 0;
  }
}
