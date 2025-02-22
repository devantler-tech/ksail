using System.CommandLine;
using System.CommandLine.IO;
using KSail.Commands.Root;
using KSail.Commands.Start;

namespace KSail.Tests.Commands.Start;

/// <summary>
/// Tests for the <see cref="KSailStartCommand"/> class.
/// </summary>
public class KSailStartCommandTests : IAsyncLifetime
{
  /// <inheritdoc/>
  public Task DisposeAsync() => Task.CompletedTask;
  /// <inheritdoc/>
  public Task InitializeAsync() => Task.CompletedTask;

  /// <summary>
  /// Tests that the 'ksail start --help'
  /// </summary>
  [Fact]
  public async Task KSailStartHelp_SucceedsAndPrintsIntroductionAndHelp()
  {
    //Arrange
    var console = new TestConsole();
    var ksailCommand = new KSailRootCommand(console);

    //Act
    int exitCode = await ksailCommand.InvokeAsync(["start", "--help"], console);

    //Assert
    Assert.Equal(0, exitCode);
    _ = await Verify(console.Error.ToString() + console.Out);
  }
}
