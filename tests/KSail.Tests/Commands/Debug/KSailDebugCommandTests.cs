using System.CommandLine;
using System.CommandLine.IO;
using KSail.Commands.Debug;
using KSail.Commands.Root;

namespace KSail.Tests.Commands.Debug;

/// <summary>
/// Tests for the <see cref="KSailDebugCommand"/> class.
/// </summary>
public class KSailDebugCommandTests : IAsyncLifetime
{
  /// <inheritdoc/>
  public Task DisposeAsync() => Task.CompletedTask;
  /// <inheritdoc/>
  public Task InitializeAsync() => Task.CompletedTask;

  /// <summary>
  /// Tests that the 'ksail debug --help'
  /// </summary>
  [Fact]
  public async Task KSailDebugHelp_SucceedsAndPrintsIntroductionAndHelp()
  {
    //Arrange
    var console = new TestConsole();
    var ksailCommand = new KSailRootCommand(console);

    //Act
    int exitCode = await ksailCommand.InvokeAsync(["debug", "--help"], console);

    //Assert
    Assert.Equal(0, exitCode);
    _ = await Verify(console.Error.ToString() + console.Out);
  }
}
