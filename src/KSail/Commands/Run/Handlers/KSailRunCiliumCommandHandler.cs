using System.CommandLine;
using Devantler.CiliumCLI;
using Devantler.Keys.Age;
using Devantler.SecretManager.Core;

namespace KSail.Commands.Run.Handlers;

class KSailRunCiliumCommandHandler(string[] args) : ICommandHandler
{
  public async Task<int> HandleAsync(CancellationToken cancellationToken)
  {
    _ = await Cilium.RunAsync(args, input: true, cancellationToken: cancellationToken).ConfigureAwait(false);
    return 0;
  }
}
