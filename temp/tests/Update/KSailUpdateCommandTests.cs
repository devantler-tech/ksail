using System.CommandLine;
using System.CommandLine.IO;
using KSail.Commands.Update;

namespace KSail.Tests.Commands.Update;

/// <summary>
/// Tests for the <see cref="KSailUpdateCommand"/> class.
/// </summary>
public class KSailUpdateCommandTests : IAsyncLifetime
{
  /// <inheritdoc/>
  public Task DisposeAsync() => Task.CompletedTask;
  /// <inheritdoc/>
  public Task InitializeAsync() => Task.CompletedTask;

  /// <summary>
  /// Tests that the 'ksail update --help'
  /// </summary>
  [Fact]
  public async Task KSailUpdateHelp_SucceedsAndPrintsIntroductionAndHelp()
  {
    //Arrange
    var console = new TestConsole();
    var ksailCommand = new KSailUpdateCommand();

    //Act
    int exitCode = await ksailCommand.InvokeAsync("--help", console);

    //Assert
    Assert.Equal(0, exitCode);
    _ = await Verify(console.Error.ToString() + console.Out);
  }
}
