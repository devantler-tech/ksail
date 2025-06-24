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
  readonly Parser _ksailCommand = new CommandLineBuilder(new KSailRootCommand(new SystemConsole()))
    .UseVersionOption()
    .UseHelp("--help")
    .UseEnvironmentVariableDirective()
    .UseParseDirective()
    .UseSuggestDirective()
    .RegisterWithDotnetSuggest()
    .UseTypoCorrections()
    .UseParseErrorReporting()
    .UseExceptionHandler()
    .CancelOnProcessTermination()
    .Build();

  readonly string[] _dependentBinariesInPath =
  [
    RuntimeInformation.IsOSPlatform(OSPlatform.Windows) ? "age-keygen.exe" : "age-keygen",
    "argocd",
    "cilium",
    RuntimeInformation.IsOSPlatform(OSPlatform.Windows) ? "flux.exe" : "flux",
    RuntimeInformation.IsOSPlatform(OSPlatform.Windows) ? "helm.exe" : "helm",
    "k3d",
    RuntimeInformation.IsOSPlatform(OSPlatform.Windows) ? "k9s.exe" : "k9s",
    "kind",
    RuntimeInformation.IsOSPlatform(OSPlatform.Windows) ? "kubeconform.exe" : "kubeconform",
    RuntimeInformation.IsOSPlatform(OSPlatform.Windows) ? "kubectl.exe" : "kubectl",
    RuntimeInformation.IsOSPlatform(OSPlatform.Windows) ? "kustomize.exe" : "kustomize",
    "sops",
    "talosctl",
  ];

  public async Task<int> RunAsync(string[] args)
  {
    bool didWriteWarning = false;
    var prevColor = Console.ForegroundColor;
    Console.ForegroundColor = ConsoleColor.Yellow;
    foreach (string binaryName in _dependentBinariesInPath)
    {
      if (!CheckBinaryIsInPath(binaryName))
      {
        Console.WriteLine($"⚠️ '{binaryName}' not found in PATH ⚠️");
        didWriteWarning = true;
      }
    }
    if (didWriteWarning)
    {
      Console.WriteLine("  - please install the missing binaries to enable all features.");
    }
    Console.ForegroundColor = prevColor;
    int exitCode = await _ksailCommand.InvokeAsync(args).ConfigureAwait(false);
    return exitCode;
  }

  public static bool CheckBinaryIsInPath(string binaryName)
  {
    string? pathEnv = Environment.GetEnvironmentVariable("PATH");

    if (!string.IsNullOrEmpty(pathEnv))
    {
      string[] paths = pathEnv.Split(Path.PathSeparator);
      foreach (string dir in paths)
      {
        string fullPath = Path.Combine(dir, binaryName);
        if (File.Exists(fullPath))
        {
          return true;
        }
      }
    }

    return false;
  }
}
