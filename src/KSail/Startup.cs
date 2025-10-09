using System.CommandLine;
using System.CommandLine.Parsing;
using System.Reflection;
using System.Runtime.InteropServices;
using KSail.Commands.Root;
using KSail.Utils;

namespace KSail;


class Startup
{
  readonly Command _ksailCommand = new KSailRootCommand();

  public async Task<int> RunAsync(string[] args)
  {
    BinaryChecker.CheckBinariesIsInPath();
    var parseResult = _ksailCommand.Parse(args);
    using var cts = new CancellationTokenSource();
    return await parseResult.InvokeAsync(parseResult.InvocationConfiguration, cts.Token).ConfigureAwait(false);
  }
}
