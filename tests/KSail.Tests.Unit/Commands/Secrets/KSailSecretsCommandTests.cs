using System.CommandLine;
using System.CommandLine.IO;
using Devantler.Keys.Age;
using KSail.Commands.Root;

namespace KSail.Tests.Unit.Commands.Secrets;


public class KSailSecretsCommandTests
{
  readonly TestConsole _console;
  readonly KSailRootCommand _ksailCommand;
  public KSailSecretsCommandTests()
  {
    _console = new TestConsole();
    _ksailCommand = new KSailRootCommand(_console);
  }

  [Theory]
  [InlineData(["secrets", "--help"])]
  [InlineData(["secrets", "encrypt", "--help"])]
  [InlineData(["secrets", "decrypt", "--help"])]
  [InlineData(["secrets", "add", "--help"])]
  [InlineData(["secrets", "rm", "--help"])]
  [InlineData(["secrets", "list", "--help"])]
  [InlineData(["secrets", "import", "--help"])]
  [InlineData(["secrets", "export", "--help"])]
  public async Task KSailSecretsHelp_SucceedsAndPrintsIntroductionAndHelp(params string[] args)
  {
    //Act
    int exitCode = await _ksailCommand.InvokeAsync(args, _console);

    //Assert
    Assert.Equal(0, exitCode);
    _ = await Verify(_console.Error.ToString() + _console.Out)
      .UseFileName($"ksail {string.Join(" ", args)}");
  }

  [Fact]
  public async Task KSailSecretsAdd_AddsANewEncryptionKeyToSOPSAgeKeyFile()
  {
    //Act
    int addExitCode = await _ksailCommand.InvokeAsync(["secrets", "add"], _console);
    string? key = _console.Out?.ToString()?.Trim();

    //Assert
    Assert.Equal(0, addExitCode);
    Assert.NotNull(key);
    Assert.NotEmpty(key);

    // Cleanup
    var ageKey = new AgeKey(key);
    int rmExitCode = await _ksailCommand.InvokeAsync(["secrets", "rm", ageKey.PublicKey], _console);
    Assert.Equal(0, rmExitCode);
  }
}
