using System.CommandLine;
using Devantler.K9sCLI;
using Devantler.Keys.Age;
using Devantler.SecretManager.Core;

namespace KSail.Commands.Run.Handlers;

class KSailRunK9sCommandHandler(string[] args) : ICommandHandler
{
  public async Task<int> HandleAsync(CancellationToken cancellationToken)
  {
    _ = await K9s.RunAsync(args, cancellationToken).ConfigureAwait(false);
    return 0;
  }
}
