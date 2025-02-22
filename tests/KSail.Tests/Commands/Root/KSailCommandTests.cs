using System.CommandLine;
using System.CommandLine.IO;
using KSail.Commands.Root;

namespace KSail.Tests.Commands.Root;

/// <summary>
/// Tests for the <see cref="KSailRootCommand"/> class.
/// </summary>
public class KSailRootCommandTests : IAsyncLifetime
{
  /// <inheritdoc/>
  public Task DisposeAsync() => Task.CompletedTask;
  /// <inheritdoc/>
  public Task InitializeAsync() => Task.CompletedTask;
  /// <summary>
  /// Tests that the 'ksail' command succeeds and returns the introduction and help text.
  /// </summary>
  [Fact]
  public async Task KSail_SucceedsAndPrintsIntroductionAndHelp()
  {
    //Arrange
    var console = new TestConsole();
    var ksailCommand = new KSailRootCommand(console);

    //Act
    int exitCode = await ksailCommand.InvokeAsync([]);

    //Assert
    _ = await Verify(console.Error.ToString() + console.Out);
    Assert.Equal(0, exitCode);
  }

  /// <summary>
  /// Tests that the 'ksail --help' command succeeds and returns the help text.
  /// </summary>
  [Fact]
  public async Task KSailHelp_SucceedsAndPrintsHelp()
  {
    //Arrange
    var console = new TestConsole();
    var ksailCommand = new KSailRootCommand(console);

    //Act
    int exitCode = await ksailCommand.InvokeAsync(["--help"], console);

    //Assert
    Assert.Equal(0, exitCode);
    _ = await Verify(console.Error.ToString() + console.Out);
  }
}
