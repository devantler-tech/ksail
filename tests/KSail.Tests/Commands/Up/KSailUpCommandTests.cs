using System.CommandLine;
using System.CommandLine.IO;
using KSail.Commands.Root;

namespace KSail.Tests.Commands.Up;


public class KSailUpCommandTests : IAsyncLifetime
{
  /// <inheritdoc/>
  public async Task DisposeAsync() => await Task.CompletedTask.ConfigureAwait(false);
  /// <inheritdoc/>
  public async Task InitializeAsync() => await Task.CompletedTask.ConfigureAwait(false);


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
