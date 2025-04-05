using System.CommandLine;
using System.CommandLine.IO;
using KSail.Commands.Root;

namespace KSail.Tests.Commands.Validate;


[Collection("KSail.Tests")]
public class KSailValidateCommandTests : IDisposable
{

  readonly TestConsole _console;
  readonly KSailRootCommand _ksailCommand;

  public KSailValidateCommandTests()
  {
    _console = new TestConsole();
    _ksailCommand = new KSailRootCommand(_console);
  }

  [Fact]
  public async Task KSailValidateHelp_SucceedsAndPrintsIntroductionAndHelp()
  {
    //Act
    int exitCode = await _ksailCommand.InvokeAsync(["validate", "--help"], _console);

    //Assert
    Assert.Equal(0, exitCode);
    _ = await Verify(_console.Error.ToString() + _console.Out);
  }


  [SkippableFact]
  public async Task KSailValidate_GivenValidPath_Succeeds()
  {
    // TODO: Add support for Windows at a later time.
    Skip.If(OperatingSystem.IsWindows(), "Skipping test on Windows OS.");

    //Act
    int initExitCode = await _ksailCommand.InvokeAsync(["init", "--name", "test-cluster"], _console).ConfigureAwait(false);
    int validateExitCode = await _ksailCommand.InvokeAsync(["validate"], _console).ConfigureAwait(false);

    //Assert
    Assert.Equal(0, initExitCode);
    Assert.Equal(0, validateExitCode);
  }


  [Fact]
  public async Task KSailValidate_GivenInvalidPathOrNoYaml_ThrowsKSailException()
  {
    //Act
    int validateExitCode = await _ksailCommand.InvokeAsync(["validate"], _console);

    //Assert
    Assert.Equal(1, validateExitCode);
  }


  [Fact]
  public async Task KSailValidate_GivenInvalidYaml_Fails()
  {
    //Arrange
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
    await File.WriteAllTextAsync(Path.Combine(Directory.GetCurrentDirectory(), "invalid.yaml"), invalidYaml);

    //Act
    int validateExitCode = await ksailCommand.InvokeAsync(["validate"], console);

    //Assert
    Assert.Equal(1, validateExitCode);
  }

  /// <inheritdoc/>
  protected virtual void Dispose(bool disposing)
  {
    if (disposing)
    {
      if (Directory.Exists("k8s"))
        Directory.Delete("k8s", true);

      foreach (string filePath in (string[])[
        "ksail.yaml",
        "kind.yaml",
        "k3d.yaml",
        ".sops.yaml"
      ])
      {
        if (File.Exists(filePath))
          File.Delete(filePath);
      }
    }
  }

  public void Dispose()
  {
    Dispose(true);
    GC.SuppressFinalize(this);
  }
}
