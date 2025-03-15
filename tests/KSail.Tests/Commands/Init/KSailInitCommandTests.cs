using System.CommandLine;
using System.CommandLine.IO;
using System.Text.RegularExpressions;
using Devantler.SecretManager.SOPS.LocalAge;
using KSail.Commands.Root;
using KSail.Utils;

namespace KSail.Tests.Commands.Init;


[Collection("KSail.Tests")]
public partial class KSailInitCommandTests : IAsyncLifetime
{
  /// <inheritdoc/>
  public Task InitializeAsync() => Task.CompletedTask;


  [Fact]
  public async Task KSailInitHelp_SucceedsAndPrintsIntroductionAndHelp()
  {
    //Arrange
    var console = new TestConsole();
    var ksailCommand = new KSailRootCommand(console);

    //Act
    int exitCode = await ksailCommand.InvokeAsync(["init", "--help"], console);

    //Assert
    Assert.Equal(0, exitCode);
    _ = await Verify(console.Error.ToString() + console.Out);
  }


  [Theory]
  [InlineData("ksail-init-native-simple", new string[] { "init" })]
  [InlineData("ksail-init-native-advanced", new string[] { "init", "--name", "ksail-advanced-native", "--secret-manager", "sops", "--cni", "cilium" })]
  [InlineData("ksail-init-k3s-simple", new string[] { "init", "--distribution", "k3s" })]
  [InlineData("ksail-init-k3s-advanced", new string[] { "init", "--name", "ksail-advanced-k3s", "--distribution", "k3s", "--secret-manager", "sops", "--cni", "cilium" })]
  public async Task KSailInit_WithVariousOptions_SucceedsAndGeneratesKSailProject(string outputDirName, string[] args)
  {
    // TODO: Add support for Windows at a later time.
    if (OperatingSystem.IsWindows())
    {
      return;
    }

    //Arrange
    string outputDir = Path.Combine(Path.GetTempPath(), outputDirName);
    var console = new TestConsole();
    var ksailCommand = new KSailRootCommand(console);
    _ = Directory.CreateDirectory(outputDir);

    //Act
    Directory.SetCurrentDirectory(outputDir);
    int exitCode = await ksailCommand.InvokeAsync(args);

    //Assert
    Assert.Equal(0, exitCode);
    Assert.True(Directory.Exists(outputDir));
    foreach (string file in Directory.GetFiles(outputDir, "*", SearchOption.AllDirectories))
    {
      string fileName = Path.GetFileName(file);
      if (fileName == ".sops.yaml")
      {
        continue;
      }
      string relativefilePath = file.Replace(outputDir, "", StringComparison.OrdinalIgnoreCase).TrimStart(Path.DirectorySeparatorChar);
      relativefilePath = relativefilePath.Replace(Path.DirectorySeparatorChar, Path.AltDirectorySeparatorChar);
      string? directoryPath = Path.GetDirectoryName(relativefilePath);
      _ = await Verify(await File.ReadAllTextAsync(file), extension: "yaml")
          .UseDirectory(Path.Combine(outputDirName, directoryPath!))
          .UseFileName(fileName)
          .ScrubLinesWithReplace(line => UrlRegex().Replace(line, "url: <url>"));
    }
  }

  [GeneratedRegex("url:.*")]
  private static partial Regex UrlRegex();

  /// <inheritdoc/>
  public async Task DisposeAsync()
  {
    var secretsManager = new SOPSLocalAgeSecretManager();

    if (File.Exists(".sops.yaml"))
    {
      var sopsConfig = await SopsConfigLoader.LoadAsync().ConfigureAwait(false);
      foreach (string? publicKey in sopsConfig.CreationRules.Select(rule => rule.Age))
      {
        try
        {
          _ = await secretsManager.DeleteKeyAsync(publicKey).ConfigureAwait(false);
        }
        catch (Exception)
        {
          //Ignore any exceptions
        }
      }
    }
    if (Directory.Exists("k8s"))
    {
      Directory.Delete("k8s", true);
    }

    string[] filePaths =
    [
      "ksail.yaml",
      "kind.yaml",
      "k3d.yaml",
      ".sops.yaml"
    ];
    foreach (string filePath in filePaths)
    {
      if (File.Exists(filePath))
      {
        File.Delete(filePath);
      }
    }
  }
}
