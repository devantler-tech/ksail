using CliWrap;
using System.CommandLine;
using System.Runtime.InteropServices;

namespace KSail.CLIWrappers;

class AgeCLIWrapper(IConsole console)
{
  readonly CLIRunner cliRunner = new(console);
  static CliWrap.Command AgeKeygen
  {
    get
    {
      string binary = (Environment.OSVersion.Platform, RuntimeInformation.ProcessArchitecture, RuntimeInformation.IsOSPlatform(OSPlatform.OSX)) switch
      {
        (PlatformID.Unix, Architecture.X64, true) => "age-keygen_darwin-amd64",
        (PlatformID.Unix, Architecture.Arm64, true) => "age-keygen_darwin-arm64",
        (PlatformID.Unix, Architecture.X64, false) => "age-keygen_linux-amd64",
        (PlatformID.Unix, Architecture.Arm64, false) => "age-keygen_linux-arm64",
        _ => throw new PlatformNotSupportedException()
      };
      return Cli.Wrap($"{AppContext.BaseDirectory}assets/binaries/{binary}");
    }
  }

  internal async Task GenerateKeyAsync()
  {
    if (!Directory.Exists($"{Environment.GetFolderPath(Environment.SpecialFolder.UserProfile)}/.ksail"))
    {
      _ = Directory.CreateDirectory($"{Environment.GetFolderPath(Environment.SpecialFolder.UserProfile)}/.ksail");
    }
    if (File.Exists($"{Environment.GetFolderPath(Environment.SpecialFolder.UserProfile)}/.ksail/ksail_sops.agekey"))
    {
      File.Delete($"{Environment.GetFolderPath(Environment.SpecialFolder.UserProfile)}/.ksail/ksail_sops.agekey");
    }
    var cmd = AgeKeygen.WithArguments($"-o {Environment.GetFolderPath(Environment.SpecialFolder.UserProfile)}/.ksail/ksail_sops.agekey");
    _ = await cliRunner.RunAsync(cmd, silent: true);
    WriteKeysToDefaultKeysTxt();
  }

  static void WriteKeysToDefaultKeysTxt()
  {
    string ksailSopsAgeKey = $"{Environment.GetFolderPath(Environment.SpecialFolder.UserProfile)}/.ksail/ksail_sops.agekey";
    if (RuntimeInformation.IsOSPlatform(OSPlatform.OSX))
    {
      string keysTxtFolder = $"{Environment.GetFolderPath(Environment.SpecialFolder.UserProfile)}/Library/Application Support/sops/age";
      string keysTxt = $"{keysTxtFolder}/keys.txt";
      AppendOrReplaceKey(ksailSopsAgeKey, keysTxtFolder, keysTxt);
    }
    else if (RuntimeInformation.IsOSPlatform(OSPlatform.Linux))
    {
      string keysTxtFolder = $"{Environment.GetFolderPath(Environment.SpecialFolder.UserProfile)}/sops/age";
      string keysTxt = $"{keysTxtFolder}/keys.txt";
      AppendOrReplaceKey(ksailSopsAgeKey, keysTxtFolder, keysTxt);
    }
  }

  static void AppendOrReplaceKey(string ksailSopsAgeKey, string keysTxtFolder, string keysTxt)
  {
    if (!Directory.Exists(keysTxtFolder))
    {
      _ = Directory.CreateDirectory(keysTxtFolder);
    }
    if (!File.Exists(keysTxt))
    {
      string[] lines = File.ReadAllLines(ksailSopsAgeKey);
      lines = lines.Prepend("# KSAIL_SOPS_KEY start").ToArray()
        .Append("# KSAIL_SOPS_KEY end").ToArray();
      File.WriteAllLines(keysTxt, lines);
    }
    else if (!File.ReadAllText(keysTxt).Contains("# KSAIL_SOPS_KEY start"))
    {
      string[] lines = File.ReadAllLines(keysTxt);
      lines = [.. lines, "# KSAIL_SOPS_KEY start", File.ReadAllText(ksailSopsAgeKey), "# KSAIL_SOPS_KEY end"];
      File.WriteAllLines(keysTxt, lines);
    }
    else
    {
      string[] lines = File.ReadAllLines(keysTxt);
      int startIndex = lines.ToList().FindIndex(line => line.Contains("# KSAIL_SOPS_KEY start"));
      int endIndex = lines.ToList().FindIndex(line => line.Contains("# KSAIL_SOPS_KEY end"));
      lines = lines.Where((_, index) => index <= startIndex || index >= endIndex).ToArray();
      lines = lines.Take(startIndex + 1).Concat(File.ReadAllLines(ksailSopsAgeKey)).Concat(lines.Skip(startIndex + 1)).ToArray();
      File.WriteAllLines(keysTxt, lines);
    }
  }
}
