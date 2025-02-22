using System.CommandLine;
using System.CommandLine.IO;
using System.Text.RegularExpressions;
using Devantler.SecretManager.SOPS.LocalAge;
using KSail.Commands.Init;
using KSail.Commands.Root;
using KSail.Utils;

namespace KSail.Tests.Commands.Init;

/// <summary>
/// Tests for the <see cref="KSailInitCommand"/> class.
/// </summary>
[Collection("KSail.Tests")]
public partial class KSailInitCommandTests : IAsyncLifetime
{
  /// <inheritdoc/>
  public Task InitializeAsync() => Task.CompletedTask;

  /// <summary>
  /// Tests that the 'ksail init' command succeeds and returns the introduction and help text.
  /// </summary>
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

  /// <summary>
  /// Tests that the 'ksail init' command with the default options succeeds and generates a KSail project.
  /// </summary>
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
    int exitCode = await ksailCommand.InvokeAsync(["init",
      "--working-directory", outputDir
    ]);

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

  /// <summary>
  /// Tests that the 'ksail init' command with the default options on top of an existing project succeeds and generates a KSail project.
  /// </summary>
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
    int exitCodeRun1 = await ksailCommand.InvokeAsync(["init",
      "--working-directory", outputDir
    ]);
    int exitCodeRun2 = await ksailCommand.InvokeAsync(["init",
      "--working-directory", outputDir
    ]);

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

  /// <summary>
  /// Tests that the 'ksail init' command with the default options and multiple clusters succeeds and generates a KSail project.
  /// </summary>
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
    int exitCodeRun1 = await ksailCommand.InvokeAsync(["init",
      "--name", "cluster1",
      "--distribution", "native",
      "--working-directory", outputDir
    ]);
    int exitCodeRun2 = await ksailCommand.InvokeAsync(["init",
      "--name", "cluster2",
      "--distribution", "k3s",
      "--working-directory", outputDir
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

  /// <summary>
  /// Tests that the 'ksail init' command advanced options succeeds and generates a KSail project.
  /// </summary>
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
    int exitCode = await ksailCommand.InvokeAsync(["init",
      "--name", "ksail-advanced-native",
      "--working-directory", outputDir,
      "--secret-manager", "sops",
      "--flux-post-build-variables",
      "--kustomize-hooks", "clusters/ksail-advanced-native", "distributions/native", "shared"
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

  /// <summary>
  /// Tests that the 'ksail init' command advanced options on top of an existing project succeeds and generates a KSail project.
  /// </summary>
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
    int exitCodeRun1 = await ksailCommand.InvokeAsync(["init",
      "--name", "ksail-advanced-native",
      "--working-directory", outputDir,
      "--secret-manager", "sops",
      "--flux-post-build-variables",
      "--kustomize-hooks", "clusters/ksail-advanced-native", "distributions/native", "shared"
    ]);
    int exitCodeRun2 = await ksailCommand.InvokeAsync(["init",
      "--name", "ksail-advanced-native",
      "--working-directory", outputDir, "--secret-manager", "sops",
      "--flux-post-build-variables",
      "--kustomize-hooks", "clusters/ksail-advanced-native","distributions/native", "shared"
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

  /// <summary>
  /// Tests that the 'ksail init' command advanced options and multiple clusters succeeds and generates a KSail project.
  /// </summary>
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
    int exitCodeRun1 = await ksailCommand.InvokeAsync(["init",
      "--name", "cluster1",
      "--working-directory", outputDir,
      "--secret-manager", "sops",
      "--flux-post-build-variables",
      "--distribution", "native",
      "--kustomize-hooks", "clusters/cluster1", "distributions/native", "shared"
    ]);
    int exitCodeRun2 = await ksailCommand.InvokeAsync(["init",
      "--name", "cluster2",
      "--working-directory", outputDir,
      "--secret-manager", "sops",
      "--flux-post-build-variables",
      "--distribution", "k3s",
      "--kustomize-hooks", "clusters/cluster2", "distributions/k3s", "shared"
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
    string[] directoryPaths =
    [
      Path.Combine(Path.GetTempPath(), "ksail-init-mixed-advanced-multi"),
      Path.Combine(Path.GetTempPath(), "ksail-init-mixed-simple-multi"),
      Path.Combine(Path.GetTempPath(), "ksail-init-native-advanced-existing"),
      Path.Combine(Path.GetTempPath(), "ksail-init-native-advanced"),
      Path.Combine(Path.GetTempPath(), "ksail-init-native-simple-existing"),
      Path.Combine(Path.GetTempPath(), "ksail-init-native-simple")
    ];
    foreach (string outputDir in directoryPaths)
    {
      if (File.Exists(Path.Combine(outputDir, ".sops.yaml")))
      {
        var sopsConfig = await SopsConfigLoader.LoadAsync(outputDir);
        foreach (string? publicKey in sopsConfig.CreationRules.Select(rule => rule.Age))
        {
          try
          {
            _ = await secretsManager.DeleteKeyAsync(publicKey);
          }
          catch (Exception)
          {
            //Ignore any exceptions
          }
        }
      }
      if (Directory.Exists(outputDir))
      {
        Directory.Delete(outputDir, true);
      }
    }
    string[] filePaths =
    [
      "ksail-config.yaml"
    ];
    foreach (string outputDir in filePaths)
    {
      if (File.Exists(outputDir))
      {
        File.Delete(outputDir);
      }
    }
  }
}
