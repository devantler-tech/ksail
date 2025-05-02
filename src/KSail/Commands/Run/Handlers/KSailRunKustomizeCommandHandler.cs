using System.CommandLine;
using Devantler.Keys.Age;
using Devantler.KustomizeCLI;
using Devantler.SecretManager.Core;

namespace KSail.Commands.Run.Handlers;

class KSailRunKustomizeCommandHandler(string[] args) : ICommandHandler
{
  public async Task<int> HandleAsync(CancellationToken cancellationToken)
  {
    _ = await Kustomize.RunAsync(args, cancellationToken: cancellationToken).ConfigureAwait(false);
    return 0;
  }
}
