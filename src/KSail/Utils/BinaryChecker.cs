using System.Runtime.InteropServices;

namespace KSail.Utils;

static class BinaryChecker
{
  public static readonly string[] DependentBinariesInPath =
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

  public static void CheckBinariesIsInPath()
  {
    bool didWriteWarning = false;
    var prevColor = Console.ForegroundColor;
    Console.ForegroundColor = ConsoleColor.Yellow;
    foreach (string binaryName in DependentBinariesInPath)
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
