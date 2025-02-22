using System.CommandLine;
using System.CommandLine.IO;
using KSail.Commands.Root;
using KSail.Commands.Up;

namespace KSail.Tests.Commands.Up;

/// <summary>
/// Tests for the <see cref="KSailUpCommand"/> class.
/// </summary>
public class KSailUpCommandTests : IAsyncLifetime
{
  /// <inheritdoc/>
  public async Task DisposeAsync() => await Task.CompletedTask;
  /// <inheritdoc/>
  public async Task InitializeAsync() => await Task.CompletedTask;

  /// <summary>
  /// Tests that the 'ksail up --help'
  /// </summary>
  [Fact]
  public async Task KSailUpHelp_SucceedsAndPrintsIntroductionAndHelp()
  {
    //Arrange
    var console = new TestConsole();
    var ksailCommand = new KSailRootCommand(console);

    //Act
    int exitCode = await ksailCommand.InvokeAsync(["up", "--help"], console);

    //Assert
    Assert.Equal(0, exitCode);
    _ = await Verify(console.Error.ToString() + console.Out);
  }
}
