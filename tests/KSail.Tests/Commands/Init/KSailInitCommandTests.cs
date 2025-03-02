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


  [Fact]
  public async Task KSailInit_WithDefaultOptions_SucceedsAndGeneratesKSailProject()
  {
    // TODO: Add support for Windows at a later time.
    if (OperatingSystem.IsWindows())
    {
      return;
    }

    //Arrange
    string outputDir = Path.Combine(Path.GetTempPath(), "ksail-init-native-simple");
    var console = new TestConsole();
    var ksailCommand = new KSailRootCommand(console);
    _ = Directory.CreateDirectory(outputDir);

    //Act
    Directory.SetCurrentDirectory(outputDir);
    int exitCode = await ksailCommand.InvokeAsync(["init"]);

    //Assert
    Assert.Equal(0, exitCode);
    Assert.True(Directory.Exists(outputDir));
    foreach (string file in Directory.GetFiles(outputDir, "*", SearchOption.AllDirectories))
    {
      string fileName = Path.GetFileName(file);
      string relativefilePath = file.Replace(outputDir, "", StringComparison.OrdinalIgnoreCase).TrimStart(Path.DirectorySeparatorChar);
      relativefilePath = relativefilePath.Replace(Path.DirectorySeparatorChar, Path.AltDirectorySeparatorChar);
      string? directoryPath = Path.GetDirectoryName(relativefilePath);
      _ = await Verify(await File.ReadAllTextAsync(file), extension: "yaml")
        .UseDirectory(Path.Combine("native-simple", directoryPath!))
        .UseFileName(fileName)
        .ScrubLinesWithReplace(line => UrlRegex().Replace(line, "url: <url>"));
    }
  }


  [Fact]
  public async Task KSailInit_WithDefaultOptionsOnTopOfExistingProject_SucceedsAndGeneratesKSailProject()
  {
    // TODO: Add support for Windows at a later time.
    if (OperatingSystem.IsWindows())
    {
      return;
    }

    //Arrange
    string outputDir = Path.Combine(Path.GetTempPath(), "ksail-init-native-simple-existing");
    var console = new TestConsole();
    var ksailCommand = new KSailRootCommand(console);
    _ = Directory.CreateDirectory(outputDir);

    //Act
    Directory.SetCurrentDirectory(outputDir);
    int exitCodeRun1 = await ksailCommand.InvokeAsync(["init"]);
    int exitCodeRun2 = await ksailCommand.InvokeAsync(["init"]);

    //Assert
    Assert.Equal(0, exitCodeRun1);
    Assert.Equal(0, exitCodeRun2);
    Assert.True(Directory.Exists(outputDir));
    foreach (string file in Directory.GetFiles(outputDir, "*", SearchOption.AllDirectories))
    {
      string fileName = Path.GetFileName(file);
      string relativefilePath = file.Replace(outputDir, "", StringComparison.OrdinalIgnoreCase).TrimStart(Path.DirectorySeparatorChar);
      relativefilePath = relativefilePath.Replace(Path.DirectorySeparatorChar, Path.AltDirectorySeparatorChar);
      string? directoryPath = Path.GetDirectoryName(relativefilePath);
      _ = await Verify(await File.ReadAllTextAsync(file), extension: "yaml")
        .UseDirectory(Path.Combine("native-simple-existing", directoryPath!))
        .UseFileName(fileName)
        .ScrubLinesWithReplace(line => UrlRegex().Replace(line, "url: <url>"));
    }
  }


  [Fact]
  public async Task KSailInit_WithDefaultOptionsMultipleClusters_SucceedsAndGeneratesKSailProject()
  {
    // TODO: Add support for Windows at a later time.
    if (OperatingSystem.IsWindows())
    {
      return;
    }

    //Arrange
    string outputDir = Path.Combine(Path.GetTempPath(), "ksail-init-mixed-simple-multi");
    var console = new TestConsole();
    var ksailCommand = new KSailRootCommand(console);
    _ = Directory.CreateDirectory(outputDir);

    //Act
    Directory.SetCurrentDirectory(outputDir);
    int exitCodeRun1 = await ksailCommand.InvokeAsync(["init",
      "--name", "cluster1",
      "--kustomization", "k8s/cluster1",
      "--distribution", "native"
    ]);
    int exitCodeRun2 = await ksailCommand.InvokeAsync(["init",
      "--name", "cluster2",
      "--kustomization", "k8s/cluster2",
      "--distribution", "k3s"
    ]);

    //Assert
    Assert.Equal(0, exitCodeRun1);
    Assert.Equal(0, exitCodeRun2);
    Assert.True(Directory.Exists(outputDir));
    foreach (string file in Directory.GetFiles(outputDir, "*", SearchOption.AllDirectories))
    {
      //Ignore any yaml paths that contain url
      string fileName = Path.GetFileName(file);
      string relativefilePath = file.Replace(outputDir, "", StringComparison.OrdinalIgnoreCase).TrimStart(Path.DirectorySeparatorChar);
      relativefilePath = relativefilePath.Replace(Path.DirectorySeparatorChar, Path.AltDirectorySeparatorChar);
      string? directoryPath = Path.GetDirectoryName(relativefilePath);
      _ = await Verify(await File.ReadAllTextAsync(file), extension: "yaml")
          .UseDirectory(Path.Combine("mixed-simple-multi", directoryPath!)
        ).UseFileName(fileName)
        .ScrubLinesWithReplace(line => UrlRegex().Replace(line, "url: <url>"));
    }
  }


  [Fact]
  public async Task KSailInit_WithAdvancedOptions_SucceedsAndGeneratesKSailProject()
  {
    // TODO: Add support for Windows at a later time.
    if (OperatingSystem.IsWindows())
    {
      return;
    }

    //Arrange
    string outputDir = Path.Combine(Path.GetTempPath(), "ksail-init-native-advanced");
    var console = new TestConsole();
    var ksailCommand = new KSailRootCommand(console);
    _ = Directory.CreateDirectory(outputDir);

    //Act
    Directory.SetCurrentDirectory(outputDir);
    int exitCode = await ksailCommand.InvokeAsync(["init",
      "--name", "ksail-advanced-native",
      "--secret-manager", "sops"
    ]);

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
          .UseDirectory(Path.Combine("native-advanced", directoryPath!)
        ).UseFileName(fileName)
        .ScrubLinesWithReplace(line => UrlRegex().Replace(line, "url: <url>"));
    }
  }


  [Fact]
  public async Task KSailInit_WithAdvancedOptionsOnTopOfExistingProject_SucceedsAndGeneratesKSailProject()
  {
    // TODO: Add support for Windows at a later time.
    if (OperatingSystem.IsWindows())
    {
      return;
    }

    //Arrange
    string outputDir = Path.Combine(Path.GetTempPath(), "ksail-init-native-advanced-existing");
    var console = new TestConsole();
    var ksailCommand = new KSailRootCommand(console);
    _ = Directory.CreateDirectory(outputDir);

    //Act
    Directory.SetCurrentDirectory(outputDir);
    int exitCodeRun1 = await ksailCommand.InvokeAsync(["init",
      "--name", "ksail-advanced-native",
      "--secret-manager", "sops"
    ]);
    int exitCodeRun2 = await ksailCommand.InvokeAsync(["init",
      "--name", "ksail-advanced-native",
      "--secret-manager", "sops"
    ]);

    //Assert
    Assert.Equal(0, exitCodeRun1);
    Assert.Equal(0, exitCodeRun2);
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
          .UseDirectory(Path.Combine("native-advanced-existing", directoryPath!)
        ).UseFileName(fileName)
        .ScrubLinesWithReplace(line => UrlRegex().Replace(line, "url: <url>"));
    }
  }


  [Fact]
  public async Task KSailInit_WithAdvancedOptionsMultipleClusters_SucceedsAndGeneratesKSailProject()
  {
    // TODO: Add support for Windows at a later time.
    if (OperatingSystem.IsWindows())
    {
      return;
    }

    //Arrange
    string outputDir = Path.Combine(Path.GetTempPath(), "ksail-init-mixed-advanced-multi");
    var console = new TestConsole();
    var ksailCommand = new KSailRootCommand(console);
    _ = Directory.CreateDirectory(outputDir);

    //Act
    Directory.SetCurrentDirectory(outputDir);
    int exitCodeRun1 = await ksailCommand.InvokeAsync(["init",
      "--name", "cluster1",
      "--kustomization", "k8s/cluster1",
      "--secret-manager", "sops",
      "--distribution", "native"
    ]);
    int exitCodeRun2 = await ksailCommand.InvokeAsync(["init",
      "--name", "cluster2",
      "--kustomization", "k8s/cluster2",
      "--secret-manager", "sops",
      "--distribution", "k3s"
    ]);

    //Assert
    Assert.Equal(0, exitCodeRun1);
    Assert.Equal(0, exitCodeRun2);
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
          .UseDirectory(Path.Combine("mixed-advanced-multi", directoryPath!)
        ).UseFileName(fileName)
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
      "ksail-config.yaml",
      "kind-config.yaml",
      "k3d-config.yaml",
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
