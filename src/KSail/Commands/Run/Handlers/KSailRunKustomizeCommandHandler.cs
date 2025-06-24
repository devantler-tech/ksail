using System.CommandLine;
using DevantlerTech.Keys.Age;
using DevantlerTech.KustomizeCLI;
using DevantlerTech.SecretManager.Core;

namespace KSail.Commands.Run.Handlers;

class KSailRunKustomizeCommandHandler(string[] args) : ICommandHandler
{
  public async Task<int> HandleAsync(CancellationToken cancellationToken)
  {
    _ = await Kustomize.RunAsync(args, cancellationToken: cancellationToken).ConfigureAwait(false);
    return 0;
  }
}
