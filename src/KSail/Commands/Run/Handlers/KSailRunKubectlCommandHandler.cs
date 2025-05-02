using System.CommandLine;
using Devantler.Keys.Age;
using Devantler.KubectlCLI;
using Devantler.SecretManager.Core;

namespace KSail.Commands.Run.Handlers;

class KSailRunKubectlCommandHandler(string[] args) : ICommandHandler
{
  public async Task<int> HandleAsync(CancellationToken cancellationToken)
  {
    _ = await Kubectl.RunAsync(args, input: true, cancellationToken: cancellationToken).ConfigureAwait(false);
    return 0;
  }
}
