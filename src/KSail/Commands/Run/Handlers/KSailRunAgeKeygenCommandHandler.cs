using System.CommandLine;
using DevantlerTech.AgeCLI;
using DevantlerTech.Keys.Age;
using DevantlerTech.SecretManager.Core;

namespace KSail.Commands.Run.Handlers;

class KSailRunAgeKeygenCommandHandler(string[] args) : ICommandHandler
{
  public async Task<int> HandleAsync(CancellationToken cancellationToken)
  {
    _ = await AgeKeygen.RunAsync(args, cancellationToken: cancellationToken).ConfigureAwait(false);
    return 0;
  }
}
