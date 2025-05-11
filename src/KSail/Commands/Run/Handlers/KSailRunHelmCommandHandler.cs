using System.CommandLine;
using Devantler.HelmCLI;
using Devantler.Keys.Age;
using Devantler.SecretManager.Core;

namespace KSail.Commands.Run.Handlers;

class KSailRunHelmCommandHandler(string[] args) : ICommandHandler
{
  public async Task<int> HandleAsync(CancellationToken cancellationToken)
  {
    _ = await Helm.RunAsync(args, input: true, cancellationToken: cancellationToken).ConfigureAwait(false);
    return 0;
  }
}
