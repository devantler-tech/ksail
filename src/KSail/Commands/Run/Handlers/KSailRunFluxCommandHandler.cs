using System.CommandLine;
using Devantler.FluxCLI;
using Devantler.Keys.Age;
using Devantler.SecretManager.Core;

namespace KSail.Commands.Run.Handlers;

class KSailRunFluxCommandHandler(string[] args) : ICommandHandler
{
  public async Task<int> HandleAsync(CancellationToken cancellationToken)
  {
    _ = await Flux.RunAsync(args, input: true, cancellationToken: cancellationToken).ConfigureAwait(false);
    return 0;
  }
}
