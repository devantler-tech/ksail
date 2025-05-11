using System.CommandLine;
using Devantler.Keys.Age;
using Devantler.SecretManager.Core;
using Devantler.SOPSCLI;

namespace KSail.Commands.Run.Handlers;

class KSailRunSopsCommandHandler(string[] args) : ICommandHandler
{
  public async Task<int> HandleAsync(CancellationToken cancellationToken)
  {
    _ = await SOPS.RunAsync(args, input: true, cancellationToken: cancellationToken).ConfigureAwait(false);
    return 0;
  }
}
