using System.CommandLine;
using System.CommandLine.IO;
using Devantler.Keys.Age;
using KSail.Commands.Root;

namespace KSail.Tests.Commands.Secrets;


public class KSailSecretsCommandTests : IAsyncLifetime
{
  /// <inheritdoc/>
  public async Task DisposeAsync() => await Task.CompletedTask.ConfigureAwait(false);
  /// <inheritdoc/>
  public async Task InitializeAsync() => await Task.CompletedTask.ConfigureAwait(false);


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
    //Arrange
    var console = new TestConsole();
    var ksailCommand = new KSailRootCommand(console);

    //Act
    int exitCode = await ksailCommand.InvokeAsync(args, console);

    //Assert
    Assert.Equal(0, exitCode);
    _ = await Verify(console.Error.ToString() + console.Out)
      .UseFileName($"ksail {string.Join(" ", args)}");
  }

  [Fact]
  public async Task KSailSecretsAdd_AddsANewEncryptionKeyToSOPSAgeKeyFile()
  {
    //Arrange
    var console = new TestConsole();
    var ksailCommand = new KSailRootCommand(console);

    //Act
    int addExitCode = await ksailCommand.InvokeAsync(["secrets", "add"], console);
    string? key = console.Out?.ToString()?.Trim();

    //Assert
    Assert.Equal(0, addExitCode);
    Assert.NotNull(key);
    Assert.NotEmpty(key);

    // Cleanup
    var ageKey = new AgeKey(key);
    int rmExitCode = await ksailCommand.InvokeAsync(["secrets", "rm", ageKey.PublicKey], console);
    Assert.Equal(0, rmExitCode);
  }
}
