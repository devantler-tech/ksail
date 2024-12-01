using System.CommandLine;
using System.CommandLine.IO;
using KSail.Commands.Down;

namespace KSail.Tests.Commands.Down;

/// <summary>
/// Tests for the <see cref="KSailDownCommand"/> class.
/// </summary>
public class KSailDownCommandTests : IAsyncLifetime
{
  /// <inheritdoc/>
  public Task DisposeAsync() => Task.CompletedTask;
  /// <inheritdoc/>
  public Task InitializeAsync() => Task.CompletedTask;

  /// <summary>
  /// Tests that the 'ksail down --help'
  /// </summary>
  [Fact]
  public async Task KSailDownHelp_SucceedsAndPrintsIntroductionAndHelp()
  {
    //Arrange
    var console = new TestConsole();
    var ksailCommand = new KSailDownCommand();

    //Act
    int exitCode = await ksailCommand.InvokeAsync("--help", console);

    //Assert
    Assert.Equal(0, exitCode);
    _ = await Verify(console.Error.ToString() + console.Out);
  }
}
