using System.CommandLine;
using System.CommandLine.IO;
using KSail.Commands.Lint;
using KSail.Commands.Root;

namespace KSail.Tests.Commands.Lint;

/// <summary>
/// Tests for the <see cref="KSailLintCommand"/> class.
/// </summary>
[Collection("KSail.Tests")]
public class KSailLintCommandTests : IAsyncLifetime
{
  /// <inheritdoc/>
  public Task InitializeAsync() => Task.CompletedTask;

  /// <summary>
  /// Tests that the 'ksail lint --help'
  /// </summary>
  [Fact]
  public async Task KSailLintHelp_SucceedsAndPrintsIntroductionAndHelp()
  {
    //Arrange
    var console = new TestConsole();
    var ksailCommand = new KSailRootCommand(console);

    //Act
    int exitCode = await ksailCommand.InvokeAsync(["lint", "--help"], console);

    //Assert
    Assert.Equal(0, exitCode);
    _ = await Verify(console.Error.ToString() + console.Out);
  }

  /// <summary>
  /// Tests that the 'ksail lint' command succeeds when given a valid path.
  /// </summary>
  [Fact]
  public async Task KSailLint_GivenValidPath_Succeeds()
  {
    //Arrange
    string path = Path.Combine(Path.GetTempPath(), "ksail-lint-test-cluster");
    var console = new TestConsole();
    var ksailCommand = new KSailRootCommand(console);

    //Act
    int initExitCode = await ksailCommand.InvokeAsync(["init", "--name", "test-cluster", "--working-directory", path], console);
    int lintExitCode = await ksailCommand.InvokeAsync(["lint", "--working-directory", path], console);

    //Assert
    Assert.Equal(0, initExitCode);
    Assert.Equal(0, lintExitCode);
  }

  /// <summary>
  /// Tests that the 'ksail lint' command skips when given an invalid path or a path with no YAML files.
  /// </summary>
  [Fact]
  public async Task KSailLint_GivenInvalidPathOrNoYaml_Succeeds()
  {
    //Arrange
    string path = Path.Combine(Path.GetTempPath(), "ksail-lint-invalid-path");
    var console = new TestConsole();
    var ksailCommand = new KSailRootCommand(console);
    _ = Directory.CreateDirectory(path);

    //Act
    int lintExitCode = await ksailCommand.InvokeAsync(["lint", "--working-directory", path], console);

    //Assert
    Assert.Equal(0, lintExitCode);
  }

  /// <summary>
  /// Tests that the 'ksail lint' command succeeds when given a valid path.
  /// </summary>
  [Fact]
  public async Task KSailLint_GivenInvalidYaml_Fails()
  {
    //Arrange
    string path = Path.Combine(Path.GetTempPath(), "ksail-lint-invalid-yaml");
    var console = new TestConsole();
    var ksailCommand = new KSailRootCommand(console);
    string invalidYaml = """
      apiVersion: v1
      kind: Pod
      metadata:
        name: my-pod
      spec:
        containers:
        - name: my-container
          image: my-image
    """;
    _ = Directory.CreateDirectory(path);
    await File.WriteAllTextAsync(Path.Combine(path, "invalid.yaml"), invalidYaml);

    //Act
    int lintExitCode = await ksailCommand.InvokeAsync(["lint", "--working-directory", path], console);

    //Assert
    Assert.Equal(1, lintExitCode);
  }

  // TODO: Fix flakyness in this test on Windows, requiring waiting for the active processes to finish.
  /// <inheritdoc/>
  public async Task DisposeAsync()
  {
    var directories = new List<string> {
      Path.Combine(Path.GetTempPath(), "ksail-lint-test-cluster"),
      Path.Combine(Path.GetTempPath(), "ksail-lint-invalid-path"),
      Path.Combine(Path.GetTempPath(), "ksail-lint-invalid-yaml")
    };
    foreach (string directory in directories)
    {
      if (Directory.Exists(directory))
      {
        bool directoryDeleted = false;
        while (!directoryDeleted)
        {
          try
          {
            Directory.Delete(directory, true);
            directoryDeleted = true;
          }
          catch (IOException)
          {
            await Task.Delay(100);
          }
        }
      }
    }
  }
}
