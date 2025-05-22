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
  readonly ExceptionHandler _exceptionHandler = new();
  readonly Parser _ksailCommand = new CommandLineBuilder(new KSailRootCommand(new SystemConsole()))
    .UseVersionOption()
    .UseHelp("--helpz")
    .UseEnvironmentVariableDirective()
    .UseParseDirective()
    .UseSuggestDirective()
    .RegisterWithDotnetSuggest()
    .UseTypoCorrections()
    .UseParseErrorReporting()
    .UseExceptionHandler()
    .CancelOnProcessTermination()
    .Build();

  public async Task<int> RunAsync(string[] args)
  {
    if (RuntimeInformation.IsOSPlatform(OSPlatform.Windows) || !string.IsNullOrEmpty(Environment.GetEnvironmentVariable("WINDOWS_TEST")))
    {
      _ = _exceptionHandler.HandleException(new PlatformNotSupportedException("KSail is not supported on Windows."));
      return 1;
    }
    else
    {
      if (!CleanUpOldKSailDotnetDirectories())
        return 1;

      int exitCode = await _ksailCommand.InvokeAsync(args).ConfigureAwait(false);
      return exitCode;
    }
  }

  bool CleanUpOldKSailDotnetDirectories()
  {
    string appDir = Path.GetDirectoryName(AppDomain.CurrentDomain.BaseDirectory) ?? string.Empty;
    string parentDir = Path.GetDirectoryName(appDir) ?? string.Empty;
    if (parentDir.EndsWith(".net/ksail", StringComparison.Ordinal))
    {
      string[] directories = Directory.GetDirectories(parentDir);
      foreach (string directory in directories)
      {
        if (directory != appDir)
        {
          try
          {
            Directory.Delete(directory, true);
          }
          catch (Exception ex)
          {
            _ = _exceptionHandler.HandleException(ex);
            return false;
          }
        }
      }
    }

    return true;
  }
}
