using System.CommandLine;
using System.CommandLine.IO;
using System.Runtime.InteropServices;
using KSail.Commands.Root;
using KSail.Utils;

namespace KSail;


class Startup
{
  readonly ExceptionHandler _exceptionHandler = new();
  readonly KSailRootCommand _ksailCommand = new(new SystemConsole());

  public async Task<int> RunAsync(string[] args)
  {
    if (RuntimeInformation.IsOSPlatform(OSPlatform.Windows) || !string.IsNullOrEmpty(Environment.GetEnvironmentVariable("WINDOWS_TEST")))
    {
      _ = _exceptionHandler.HandleException(new PlatformNotSupportedException("KSail is not supported on Windows."));
      return 1;
    }
    else
    {
      int exitCode = await _ksailCommand.InvokeAsync(args).ConfigureAwait(false);
      return exitCode;
    }
  }
}
