using System.CommandLine;
using DevantlerTech.Keys.Age;
using DevantlerTech.KindCLI;
using DevantlerTech.SecretManager.Core;

namespace KSail.Commands.Run.Handlers;

class KSailRunKindCommandHandler(string[] args) : ICommandHandler
{
  public async Task<int> HandleAsync(CancellationToken cancellationToken)
  {
    _ = await Kind.RunAsync(args, input: true, cancellationToken: cancellationToken).ConfigureAwait(false);
    return 0;
  }
}
