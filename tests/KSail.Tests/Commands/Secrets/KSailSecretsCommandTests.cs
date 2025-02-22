using System.CommandLine;
using System.CommandLine.IO;
using KSail.Commands.Root;
using KSail.Commands.Secrets;

namespace KSail.Tests.Commands.Secrets;

/// <summary>
/// Tests for the <see cref="KSailSecretsCommand"/> class.
/// </summary>
public class KSailSecretsCommandTests : IAsyncLifetime
{
  /// <inheritdoc/>
  public async Task DisposeAsync() => await Task.CompletedTask;
  /// <inheritdoc/>
  public async Task InitializeAsync() => await Task.CompletedTask;

  /// <summary>
  /// Tests that the 'ksail up --help'
  /// </summary>
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
}
