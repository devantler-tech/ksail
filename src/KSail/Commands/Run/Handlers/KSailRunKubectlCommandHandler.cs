using System.CommandLine;
using DevantlerTech.Keys.Age;
using DevantlerTech.KubectlCLI;
using DevantlerTech.SecretManager.Core;

namespace KSail.Commands.Run.Handlers;

class KSailRunKubectlCommandHandler(string[] args) : ICommandHandler
{
  public async Task<int> HandleAsync(CancellationToken cancellationToken)
  {
    _ = await Kubectl.RunAsync(args, input: true, cancellationToken: cancellationToken).ConfigureAwait(false);
    return 0;
  }
}
