using System.CommandLine;
using KSail.Commands.Root;

namespace KSail.Tests.Unit.Commands.Validate;

public class KSailValidateCommandTests
{
  readonly Command _ksailCommand;

  public KSailValidateCommandTests() => _ksailCommand = new KSailRootCommand();

  [Fact]
  public async Task KSailValidateHelp_SucceedsAndPrintsIntroductionAndHelp()
  {
    //Act
    var outputWriter = new StringWriter();
    var errorWriter = new StringWriter();
    using var cts = new CancellationTokenSource();
    var commandLineConfiguration = new CommandLineConfiguration(_ksailCommand)
    {
      Output = outputWriter,
      Error = errorWriter
    };
    int exitCode = await _ksailCommand.Parse(["validate", "-h"], commandLineConfiguration).InvokeAsync(cts.Token);

    //Assert
    Assert.Equal(0, exitCode);
    _ = await Verify(errorWriter.ToString() + outputWriter.ToString());
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
    var outputWriter = new StringWriter();
    var errorWriter = new StringWriter();
    using var cts = new CancellationTokenSource();
    var commandLineConfiguration = new CommandLineConfiguration(_ksailCommand)
    {
      Output = outputWriter,
      Error = errorWriter
    };
    int initExitCode = await _ksailCommand.Parse(["init", "--output", $"{tempDir}", "--name", "test-cluster"], commandLineConfiguration).InvokeAsync(cts.Token).ConfigureAwait(false);
    int validateExitCode = await _ksailCommand.Parse(["validate", "--path", tempDir], commandLineConfiguration).InvokeAsync(cts.Token).ConfigureAwait(false);

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
    var outputWriter = new StringWriter();
    var errorWriter = new StringWriter();
    using var cts = new CancellationTokenSource();
    var commandLineConfiguration = new CommandLineConfiguration(_ksailCommand)
    {
      Output = outputWriter,
      Error = errorWriter
    };
    int validateExitCode = await _ksailCommand.Parse(["validate", "-kp", tempDir], commandLineConfiguration).InvokeAsync(cts.Token);

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
    var outputWriter = new StringWriter();
    var errorWriter = new StringWriter();
    using var cts = new CancellationTokenSource();
    var commandLineConfiguration = new CommandLineConfiguration(_ksailCommand)
    {
      Output = outputWriter,
      Error = errorWriter
    };
    int validateExitCode = await _ksailCommand.Parse(["validate", "--path", tempDir], commandLineConfiguration).InvokeAsync(cts.Token);

    //Assert
    Assert.Equal(1, validateExitCode);

    //Cleanup
    Directory.Delete(tempDir, true);
  }
}
