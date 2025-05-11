using System.CommandLine;
using System.CommandLine.IO;
using KSail.Commands.Root;

namespace KSail.Tests.Unit.Commands.Validate;
public class KSailValidateCommandTests
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
    int exitCode = await _ksailCommand.InvokeAsync(["validate", "-h"], _console);

    //Assert
    Assert.Equal(0, exitCode);
    _ = await Verify(_console.Error.ToString() + _console.Out);
  }


  [SkippableFact]
  public async Task KSailValidate_GivenValidPath_Succeeds()
  {
    // TODO: Add support for Windows at a later time.
    Skip.If(OperatingSystem.IsWindows(), "Skipping test on Windows OS.");
    //Arrange
    string tempDir = Path.Combine(Path.GetTempPath(), "ksail-validate-given-valid-path-test");
    _ = Directory.CreateDirectory(tempDir);

    //Act
    int initExitCode = await _ksailCommand.InvokeAsync(["init", "--output", $"{tempDir}", "--name", "test-cluster"], _console).ConfigureAwait(false);
    int validateExitCode = await _ksailCommand.InvokeAsync(["validate", "--path", tempDir], _console).ConfigureAwait(false);

    //Assert
    Assert.Equal(0, initExitCode);
    Assert.Equal(0, validateExitCode);

    //Cleanup
    Directory.Delete(tempDir, true);
  }


  [Fact]
  public async Task KSailValidate_GivenInvalidPathOrNoYaml_ThrowsKSailException()
  {
    //Arrange
    // Create a temporary directory for the test
    string tempDir = Path.Combine(Path.GetTempPath(), "ksail-validate-given-invalid-path-test");
    _ = Directory.CreateDirectory(tempDir);

    //Act
    int validateExitCode = await _ksailCommand.InvokeAsync(["validate", "-kp", tempDir], _console);

    //Assert
    Assert.Equal(1, validateExitCode);

    //Cleanup
    Directory.Delete(tempDir, true);
  }


  [Fact]
  public async Task KSailValidate_GivenInvalidYaml_Fails()
  {
    //Arrange
    string tempDir = Path.Combine(Path.GetTempPath(), "ksail-validate-given-invalid-yaml-test");
    _ = Directory.CreateDirectory(tempDir);

    //Act
    string invalidYaml = """
    ---
    name: invalid-yaml
    - name: infrastructure
    """;
    await File.WriteAllTextAsync(Path.Combine(tempDir, "invalid.yaml"), invalidYaml);
    int validateExitCode = await _ksailCommand.InvokeAsync(["validate", "--path", tempDir], _console);

    //Assert
    Assert.Equal(1, validateExitCode);

    //Cleanup
    Directory.Delete(tempDir, true);
  }
}
