using System.CommandLine;
using System.CommandLine.IO;
using KSail.Commands.Root;
using KSail.Commands.Stop;

namespace KSail.Tests.Commands.Stop;

/// <summary>
/// Tests for the <see cref="KSailStopCommand"/> class.
/// </summary>
public class KSailStopCommandTests : IAsyncLifetime
{
  /// <inheritdoc/>
  public Task DisposeAsync() => Task.CompletedTask;
  /// <inheritdoc/>
  public Task InitializeAsync() => Task.CompletedTask;

  /// <summary>
  /// Tests that the 'ksail stop --help'
  /// </summary>
  [Fact]
  public async Task KSailStopHelp_SucceedsAndPrintsIntroductionAndHelp()
  {
    //Arrange
    var console = new TestConsole();
    var ksailCommand = new KSailRootCommand(console);

    //Act
    int exitCode = await ksailCommand.InvokeAsync(["stop", "--help"], console);

    //Assert
    Assert.Equal(0, exitCode);
    _ = await Verify(console.Error.ToString() + console.Out);
  }
}
