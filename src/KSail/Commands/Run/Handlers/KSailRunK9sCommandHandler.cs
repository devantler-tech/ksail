using System.CommandLine;
using DevantlerTech.K9sCLI;
using DevantlerTech.Keys.Age;
using DevantlerTech.SecretManager.Core;

namespace KSail.Commands.Run.Handlers;

class KSailRunK9sCommandHandler(string[] args) : ICommandHandler
{
  public async Task<int> HandleAsync(CancellationToken cancellationToken)
  {
    _ = await K9s.RunAsync(args, cancellationToken).ConfigureAwait(false);
    return 0;
  }
}
