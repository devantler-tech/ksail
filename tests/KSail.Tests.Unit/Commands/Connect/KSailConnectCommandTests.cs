using System.CommandLine;
using System.CommandLine.Parsing;
using KSail.Commands.Root;

namespace KSail.Tests.Unit.Commands.Connect;


public class KSailConnectCommandTests
{
  [Fact]
  public async Task KSailDebugHelp_SucceedsAndPrintsIntroductionAndHelp()
  {
    //Arrange
    var console = new TestConsole();
    var ksailCommand = new KSailRootCommand(console);

    //Act
    int exitCode = await ksailCommand.InvokeAsync(["connect", "--help"], console);

    //Assert
    Assert.Equal(0, exitCode);
    _ = await Verify(console.Error.ToString() + console.Out);
  }
}
