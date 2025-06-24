using System.CommandLine;
using System.CommandLine.Builder;
using System.CommandLine.IO;
using System.CommandLine.Parsing;
using System.Reflection;
using System.Runtime.InteropServices;
using KSail.Commands.Root;
using KSail.Utils;

namespace KSail;


class Startup
{
  readonly Command _ksailCommand = new KSailRootCommand(new SystemConsole());

  public async Task<int> RunAsync(string[] args)
  {
    BinaryChecker.CheckBinariesIsInPath();
    int exitCode = await _ksailCommand.InvokeAsync(args).ConfigureAwait(false);
    return exitCode;
  }
}
