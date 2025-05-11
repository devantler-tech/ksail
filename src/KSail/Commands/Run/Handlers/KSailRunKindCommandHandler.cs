using System.CommandLine;
using Devantler.Keys.Age;
using Devantler.KindCLI;
using Devantler.SecretManager.Core;

namespace KSail.Commands.Run.Handlers;

class KSailRunKindCommandHandler(string[] args) : ICommandHandler
{
  public async Task<int> HandleAsync(CancellationToken cancellationToken)
  {
    _ = await Kind.RunAsync(args, input: true, cancellationToken: cancellationToken).ConfigureAwait(false);
    return 0;
  }
}
