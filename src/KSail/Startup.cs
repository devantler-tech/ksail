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
    _ = await parseResult.InvokeAsync(cts.Token).ConfigureAwait(false);
    return 0;
  }
}
