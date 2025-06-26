using System.CommandLine;
using KSail.Commands.Root;

namespace KSail.Tests.Unit.Commands.Update;


public class KSailUpdateCommandTests
{
  [Fact]
  public async Task KSailUpdateHelp_SucceedsAndPrintsIntroductionAndHelp()
  {
    //Arrange
    var console = new TestConsole();
    var ksailCommand = new KSailRootCommand(console);

    //Act
    int exitCode = await ksailCommand.InvokeAsync(["update", "-h"], console);

    //Assert
    Assert.Equal(0, exitCode);
    _ = await Verify(console.Error.ToString() + console.Out);
  }
}
